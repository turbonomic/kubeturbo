package framework

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
)

const (
	testConfigQPS             = 80
	testConfigBurst           = 100
	DockerImagePullSecretName = "integration-test"
)

type TestFramework struct {
	testNamespaceName string

	Config     *restclient.Config
	Kubeconfig *clientcmdapi.Config

	BaseName string
}

func NewTestFramework(baseName string) *TestFramework {
	f := &TestFramework{
		BaseName: baseName,
	}
	// AfterEach(f.AfterEach)
	// BeforeEach(f.BeforeEach)
	return f
}

func (f *TestFramework) TestNamespaceName() string {
	if f.testNamespaceName == "" {
		client := f.GetKubeClient(fmt.Sprintf("%s-create-namespace", f.BaseName))
		f.testNamespaceName = CreateTestNamespace(client, f.BaseName)
		if TestContext.IsIstioEnabled {
			patchIstioInjectionLabelToNamespace(client, f.testNamespaceName)
		}
	}
	return f.testNamespaceName
}

// BeforeEach reads the cluster configuration if it has not yet been read.
func (f *TestFramework) BeforeEach() {
	if f.Config == nil {
		By("Reading cluster configuration")
		var err error
		f.Config, f.Kubeconfig, err = loadConfig(TestContext.KubeConfig, TestContext.KubeContext)
		Expect(err).NotTo(HaveOccurred())
	}
}

// AfterEach deletes the namespace, after reading its events.
func (f *TestFramework) AfterEach() {
	userAgent := fmt.Sprintf("%s-teardown", f.BaseName)
	client := f.GetKubeClient(userAgent)
	DeleteNamespace(client, f.testNamespaceName)
}

func (f *TestFramework) GetKubeConfig() *restclient.Config {
	return f.Config
}

func (f *TestFramework) GetKubeClient(userAgent string) *kubeclientset.Clientset {
	config := restclient.CopyConfig(f.Config)
	restclient.AddUserAgent(config, userAgent)
	return kubeclientset.NewForConfigOrDie(config)
}

func (f *TestFramework) GetClusterNodes() []string {
	client := f.GetKubeClient(fmt.Sprintf("%s-cluster", f.BaseName))
	nodeNames := []string{}
	nodes, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	ExpectNoError(err, fmt.Sprintf("Error retrieving list of cluster nodes: %+v", err))

	for _, node := range nodes.Items {
		// skip master nodes and NotReady nodes
		if isMasterNode(node) || !util.NodeIsReady(&node) {
			continue
		}
		nodeNames = append(nodeNames, node.Name)
	}
	return nodeNames
}

func isMasterNode(node corev1.Node) bool {
	for key := range node.Labels {
		if key == "node-role.kubernetes.io/master" {
			return true
		}
	}
	return false
}

func loadConfig(configPath, context string) (*restclient.Config, *clientcmdapi.Config, error) {
	Logf(">>> kubeConfig: %s", configPath)
	c, err := clientcmd.LoadFromFile(configPath)
	if err != nil {
		return nil, nil, errors.Errorf("error loading kubeConfig %s: %v", configPath, err.Error())
	}
	if context != "" {
		Logf(">>> kubeContext: %s", context)
		c.CurrentContext = context
	}
	cfg, err := clientcmd.NewDefaultClientConfig(*c, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, nil, errors.Errorf("error creating default client config: %v", err.Error())
	}
	cfg.QPS = testConfigQPS
	cfg.Burst = testConfigBurst
	return cfg, c, nil
}

func DeleteNamespace(client kubeclientset.Interface, namespaceName string) {
	orphanDependents := false
	if err := client.CoreV1().Namespaces().Delete(context.TODO(), namespaceName, metav1.DeleteOptions{OrphanDependents: &orphanDependents}); err != nil {
		if !apierrors.IsNotFound(err) {
			Failf("Error while deleting namespace %s: %s", namespaceName, err)
		}
	}

	waitForNamespaceDeletion(client, namespaceName)
}

func CreateTestNamespace(client kubeclientset.Interface, baseName string) string {
	By("Creating a namespace to execute the test in")
	namespaceName, err := CreateNamespace(client, fmt.Sprintf("kubeturbo-test-%v-", baseName))
	Expect(err).NotTo(HaveOccurred())
	By(fmt.Sprintf("Created test namespace %s", namespaceName))
	return namespaceName
}

func CreateNamespace(client kubeclientset.Interface, generateName string) (string, error) {
	namespaceObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName,
		},
	}

	var namespaceName string
	if err := wait.PollImmediate(PollInterval, TestContext.SingleCallTimeout, func() (bool, error) {
		namespace, err := client.CoreV1().Namespaces().Create(context.TODO(), namespaceObj, metav1.CreateOptions{})
		if err != nil {
			Logf("Unexpected error while creating namespace: %v", err)
			return false, nil
		}
		namespaceName = namespace.Name
		return true, nil
	}); err != nil {
		return "", err
	}
	return namespaceName, nil
}

func waitForNamespaceDeletion(client kubeclientset.Interface, namespace string) error {
	err := wait.PollImmediate(PollInterval, TestContext.SingleCallTimeout, func() (bool, error) {
		if _, err := client.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			Errorf("Error while waiting for namespace to be removed: %v", err)
		}
		return false, nil
	})
	if err != nil {
		return errors.Errorf("Namespace %q was not deleted after %v", namespace, TestContext.SingleCallTimeout)
	}
	return nil
}

func (f *TestFramework) GenerateCustomImagePullSecret(nsName string) error {
	if TestContext.DockerUserName != "" && TestContext.DockerUserPwd != "" {
		client := f.GetKubeClient(fmt.Sprintf("%s-create-namespace", f.BaseName))
		_, err := client.CoreV1().Secrets(nsName).Create(
			context.TODO(),
			dockerConfigSecret(DockerImagePullSecretName, nsName, TestContext.DockerRegistry, TestContext.DockerUserName, TestContext.DockerUserPwd),
			metav1.CreateOptions{},
		)
		if err != nil {
			Logf("Failed to create secret in the namespace <%s>: %v", nsName, err)
			return err
		}
	}
	return nil
}

func dockerConfigSecret(secName, nsName, dockerRegistry, dockerUserName, dockerUserPassword string) *corev1.Secret {
	dockerConfig := fmt.Sprintf(`{"auths":{"%s":{"username":"%s","password":"%s"}}}`, dockerRegistry, dockerUserName, dockerUserPassword)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secName,
			Namespace: nsName,
		},
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(dockerConfig),
		},
		Type: corev1.SecretTypeDockerConfigJson,
	}
}

func patchIstioInjectionLabelToNamespace(client kubeclientset.Interface, nsName string) error {
	injectedLabel := `{"metadata": {"labels": {"istio-injection": "enabled"}}}`
	_, err := client.CoreV1().Namespaces().Patch(context.TODO(), nsName, types.MergePatchType, []byte(injectedLabel), metav1.PatchOptions{})
	return err
}
