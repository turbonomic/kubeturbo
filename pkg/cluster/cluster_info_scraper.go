package cluster

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
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

var (
	labelSelectEverything = labels.Everything().String()
	fieldSelectEverything = fields.Everything().String()
)

type ClusterScraper struct {
	*client.Clientset
}

func NewClusterInfoScraper(kubeConfig *restclient.Config) (*ClusterScraper, error) {
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeClient: %s", err)
	}

	return &ClusterScraper{
		Clientset: kubeClient,
	}, nil
}

func (s *ClusterScraper) GetAllNodes() ([]*api.Node, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labelSelectEverything,
		FieldSelector: fieldSelectEverything,
	}
	return s.GetNodes(listOption)
}

func (s *ClusterScraper) GetNodes(opts metav1.ListOptions) ([]*api.Node, error) {
	nodeList, err := s.CoreV1().Nodes().List(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list all nodes in the cluster: %s", err)
	}
	var nodes []*api.Node
	for _, n := range nodeList.Items {
		node := n
		nodes = append(nodes, &node)
	}

	return nodes, nil
}

func (s *ClusterScraper) GetAllPods() ([]*api.Pod, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labelSelectEverything,
		FieldSelector: fieldSelectEverything,
	}
	return s.GetPods(api.NamespaceAll, listOption)
}

func (s *ClusterScraper) GetPods(namespaces string, opts metav1.ListOptions) ([]*api.Pod, error) {
	podList, err := s.CoreV1().Pods(namespaces).List(opts)
	if err != nil {
		return nil, err
	}

	pods := []*api.Pod{}
	for _, p := range podList.Items {
		pod := p
		pods = append(pods, &pod)
	}
	return pods, nil
}

func (s *ClusterScraper) GetAllServices() ([]*api.Service, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labelSelectEverything,
	}

	return s.GetServices(api.NamespaceAll, listOption)
}

func (s *ClusterScraper) GetServices(namespace string, opts metav1.ListOptions) ([]*api.Service, error) {
	serviceList, err := s.CoreV1().Services(namespace).List(opts)
	if err != nil {
		return nil, err
	}

	services := []*api.Service{}
	for _, s := range serviceList.Items {
		service := s
		services = append(services, &service)
	}
	return services, nil
}

func (s *ClusterScraper) GetEndpoints(namespaces string, opts metav1.ListOptions) ([]*api.Endpoints, error) {
	epList, err := s.CoreV1().Endpoints(namespaces).List(opts)
	if err != nil {
		return nil, err
	}

	endpoints := []*api.Endpoints{}
	for _, ep := range epList.Items {
		endpoint := ep
		endpoints = append(endpoints, &endpoint)
	}
	return endpoints, nil
}

func (s *ClusterScraper) GetAllEndpoints() ([]*api.Endpoints, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labelSelectEverything,
	}
	return s.GetEndpoints(api.NamespaceAll, listOption)
}

func (s *ClusterScraper) GetKubernetesServiceID() (svcID string, err error) {
	svc, err := s.CoreV1().Services(k8sDefaultNamespace).Get(kubernetesServiceName, metav1.GetOptions{})
	if err != nil {
		return
	}
	svcID = string(svc.UID)
	return
}

func (s *ClusterScraper) GetRunningPodsOnNodes(nodeList []*api.Node) []*api.Pod {
	pods := []*api.Pod{}
	for _, node := range nodeList {
		nodeRunningPodsList, err := s.findRunningPodsOnNode(node.Name)
		if err != nil {
			glog.Errorf("Failed to find running pods in %s", node.Name)
			continue
		}
		pods = append(pods, nodeRunningPodsList...)
	}
	return pods
}

// TODO, create a local pod, node cache to avoid too many API request.
func (s *ClusterScraper) findRunningPodsOnNode(nodeName string) ([]*api.Pod, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName + ",status.phase=" +
		string(api.PodRunning))
	if err != nil {
		return nil, err
	}
	podList, err := s.Pods(api.NamespaceAll).List(metav1.ListOptions{FieldSelector: fieldSelector.String()})
	if err != nil {
		return nil, err
	}
	pods := []*api.Pod{}
	for _, p := range podList.Items {
		pod := p
		pods = append(pods, &pod)
	}
	return pods, nil
}
