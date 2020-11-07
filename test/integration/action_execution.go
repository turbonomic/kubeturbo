package integration

import (
	"fmt"
	"strings"

	"github.com/turbonomic/kubeturbo/test/integration/framework"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"
	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
)

var _ = Describe("Action Executor", func() {
	f := framework.NewTestFramework("action-executor")
	var kubeConfig *restclient.Config
	var namespace string
	var actionHandler *action.ActionHandler
	var kubeClient *kubeclientset.Clientset

	//AfterSuite(f.AfterEach)
	BeforeEach(func() {
		f.BeforeEach()
		// The following setup is shared across tests here
		if kubeConfig == nil {

			kubeConfig := f.GetKubeConfig()
			kubeClient = f.GetKubeClient("action-executor")
			dynamicClient, err := dynamic.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate dynamic client for kubernetes test cluster: %v", err)
			}

			cluster.NewClusterScraper(kubeClient, dynamicClient)
			actionHandlerConfig := action.NewActionHandlerConfig("", nil, nil,
				cluster.NewClusterScraper(kubeClient, dynamicClient), nil, nil, true)

			actionHandler = action.NewActionHandler(actionHandlerConfig)
		}
		namespace = f.TestNamespaceName()
	})

	Describe("executing action move pod", func() {
		It("should result in new pod on target node", func() {
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, 1))
			framework.ExpectNoError(err, "Error creating test resources")

			pod, err := getDeploymentsPod(kubeClient, dep.Name, namespace, "")
			framework.ExpectNoError(err, "Error getting deployments pod")
			// This should not happen. We should ideally get a pod.
			if pod == nil {
				framework.Failf("Failed to find a pod for deployment: %s", dep.Name)
			}

			targetNodeName := getTargetSENodeName(f, pod)
			if targetNodeName == "" {
				framework.Failf("Failed to find a pod for deployment: %s", dep.Name)
			}

			_, err = actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_MOVE,
				newTargetSEFromPod(pod), newHostSEFromNodeName(targetNodeName)), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Move action failed")

			validateMovedPod(kubeClient, dep.Name, namespace, targetNodeName)

		})
	})

	// TODO: this particular Describe is currently used as the teardown for this
	// whole test (not the suite).
	// This will work only if run sequentially. Find a better way to do this.
	Describe("test teardowon", func() {
		It(fmt.Sprintf("Deleting framework namespace: %s", namespace), func() {
			f.AfterEach()
		})
	})
})

func createDeployResource(client *kubeclientset.Clientset, dep *appsv1.Deployment) (*appsv1.Deployment, error) {
	newDep, err := createDeployment(client, dep)
	if err != nil {
		return nil, err
	}
	return waitForDeployment(client, newDep.Name, newDep.Namespace)
}

// This can also be bootstrapped from a test resource directory
// which holds yaml files.
func depSingleContainerWithResources(namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-app",
				},
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-app",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "test-cont",
							Image:   "busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "while true; do sleep 30; done;"},
							Resources: corev1.ResourceRequirements{
								Limits: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("200Mi"),
								},
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("100Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func createDeployment(client kubeclientset.Interface, dep *appsv1.Deployment) (*appsv1.Deployment, error) {
	var newDep *appsv1.Deployment
	var err error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDep, err = client.AppsV1().Deployments(dep.Namespace).Create(dep)
		if err != nil {
			glog.Errorf("Unexpected error while creating deployment: %v", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	return newDep, nil
}

func waitForDeployment(client kubeclientset.Interface, depName, namespace string) (*appsv1.Deployment, error) {
	var newDep *appsv1.Deployment
	var err error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDep, err = client.AppsV1().Deployments(namespace).Get(depName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Unexpected error while getting deployment: %v", err)
			return false, nil
		}
		if newDep.Status.AvailableReplicas == *newDep.Spec.Replicas {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	return newDep, nil
}

// We create a deployment with only 1 replica, so we should be able to get
// the only pod using the name as prefix
func getDeploymentsPod(client kubeclientset.Interface, depName, namespace, targetNodeName string) (*corev1.Pod, error) {
	pods, err := client.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, depName) {
			if targetNodeName != "" && targetNodeName != pod.Spec.NodeName {
				continue
			}
			return &pod, nil
		}
	}

	return nil, nil
}

func validateMovedPod(client kubeclientset.Interface, depName, namespace, targetNodeName string) {
	pod, err := getDeploymentsPod(client, depName, namespace, targetNodeName)
	framework.ExpectNoError(err, "Get moved pod failed.")

	if pod.Spec.NodeName != targetNodeName {
		framework.Failf("Pod move failed. Pods node: %s vs target node: %s", pod.Spec.NodeName, targetNodeName)
	}
}

type mockProgressTrack struct{}

func (p *mockProgressTrack) UpdateProgress(actionState proto.ActionResponseState, description string, progress int32) {
}

func newActionExecutionDTO(actionType proto.ActionItemDTO_ActionType, targetSE, newHostSE *proto.EntityDTO) *proto.ActionExecutionDTO {
	ai := &proto.ActionItemDTO{}
	ai.TargetSE = targetSE
	ai.NewSE = newHostSE
	ai.ActionType = &actionType
	dto := &proto.ActionExecutionDTO{}
	dto.ActionItem = []*proto.ActionItemDTO{ai}

	return dto
}

func newTargetSEFromPod(pod *corev1.Pod) *proto.EntityDTO {
	entityType := proto.EntityDTO_CONTAINER_POD
	podDispName := podID(pod)
	podId := string(pod.UID)

	se := &proto.EntityDTO{}
	se.EntityType = &entityType
	se.DisplayName = &podDispName
	se.Id = &podId

	return se
}

func newHostSEFromNodeName(nodeName string) *proto.EntityDTO {
	entityType := proto.EntityDTO_VIRTUAL_MACHINE
	nodeDispName := nodeName

	se := &proto.EntityDTO{}
	se.EntityType = &entityType
	se.DisplayName = &nodeDispName

	return se
}

func getTargetSENodeName(f *framework.TestFramework, pod *corev1.Pod) string {
	for _, nodeName := range f.GetClusterNodes() {
		if nodeName != pod.Spec.NodeName {
			return nodeName
		}
	}

	return ""
}

func podID(pod *corev1.Pod) string {
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}
