package master

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	restclient "k8s.io/client-go/rest"

	"fmt"
	"github.com/golang/glog"
)

const (
	k8sDefaultNamespace = "default"

	kubernetesServiceName = "kubernetes"
)

type ClusterInfoScraper struct {
	kubeClient *client.Clientset
}

func newClusterInfoScraper(kubeConfig *restclient.Config) (*ClusterInfoScraper, error) {
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeClient: %s", err)
	}

	return &ClusterInfoScraper{
		kubeClient: kubeClient,
	}, nil
}

func GetKubernetesServiceID(kubeClient *client.Clientset) (svcID string, err error) {
	svc, err := kubeClient.CoreV1().Services(k8sDefaultNamespace).Get(kubernetesServiceName, metav1.GetOptions{})
	if err != nil {
		return
	}
	svcID = string(svc.UID)
	return
}

// Discover pods running on nodes specified in task.
func (s *ClusterInfoScraper) groupPodsByNodeNames(nodeList []*api.Node) map[string]*api.PodList {
	groupedPods := make(map[string]*api.PodList)
	for _, node := range nodeList {
		nodeNonTerminatedPodsList, err := s.findRunningPods(node.Name)
		if err != nil {
			glog.Errorf("Failed to find non-ternimated pods in %s", node.Name)
			continue
		}
		groupedPods[node.Name] = nodeNonTerminatedPodsList
	}
	return groupedPods
}

// TODO, create a local pod, node cache to avoid too many API request.
func (s *ClusterInfoScraper) findRunningPods(nodeName string) (*api.PodList, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName + ",status.phase=" +
		string(api.PodRunning))
	if err != nil {
		return nil, err
	}
	return s.kubeClient.Pods(api.NamespaceAll).List(metav1.ListOptions{FieldSelector: fieldSelector.String()})
}
