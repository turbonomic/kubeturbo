package cluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	restclient "k8s.io/client-go/rest"

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

func NewClusterScraper(kclient *client.Clientset) *ClusterScraper {
	return &ClusterScraper{
		Clientset: kclient,
	}
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

func (s *ClusterScraper) GetNamespaces() ([]*api.Namespace, error) {
	namespaceList, err := s.Core().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list all namespaces in the cluster: %s", err)
	}
	namespaces := make([]*api.Namespace, len(namespaceList.Items))
	for i := 0; i < len(namespaceList.Items); i++ {
		namespaces[i] = &namespaceList.Items[i]
	}
	return namespaces, nil
}

func (s *ClusterScraper) GetResourceQuotas() ([]*api.ResourceQuota, error) {
	namespace := api.NamespaceAll
	quotaList, err := s.Core().ResourceQuotas(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list all quotas in the cluster: %s", err)
	}
	quotas := make([]*api.ResourceQuota, len(quotaList.Items))
	for i := 0; i < len(quotaList.Items); i++ {
		quotas[i] = &quotaList.Items[i]
	}
	return quotas, nil
}

// Return a map containing namespace and the list of quotas defined in the namespace.
func (s *ClusterScraper) GetNamespaceQuotas() (map[string][]*api.ResourceQuota, error) {
	quotaList, err := s.GetResourceQuotas()
	if err != nil {
		return nil, fmt.Errorf("%s", err)
	}

	quotaMap := make(map[string][]*api.ResourceQuota)
	for _, item := range quotaList {
		quotaList, exists := quotaMap[item.Namespace]
		if !exists {
			quotaList = []*api.ResourceQuota{}
		}
		quotaList = append(quotaList, item)
		quotaMap[item.Namespace] = quotaList
	}
	return quotaMap, nil
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
	nodes := make([]*api.Node, len(nodeList.Items))
	for i := 0; i < len(nodeList.Items); i++ {
		nodes[i] = &nodeList.Items[i]
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

	pods := make([]*api.Pod, len(podList.Items))
	for i := 0; i < len(podList.Items); i++ {
		pods[i] = &podList.Items[i]
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

	services := make([]*api.Service, len(serviceList.Items))
	for i := 0; i < len(serviceList.Items); i++ {
		services[i] = &serviceList.Items[i]
	}
	return services, nil
}

func (s *ClusterScraper) GetEndpoints(namespaces string, opts metav1.ListOptions) ([]*api.Endpoints, error) {
	epList, err := s.CoreV1().Endpoints(namespaces).List(opts)
	if err != nil {
		return nil, err
	}

	endpoints := make([]*api.Endpoints, len(epList.Items))
	for i := 0; i < len(epList.Items); i++ {
		endpoints[i] = &epList.Items[i]
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
	pods := make([]*api.Pod, len(podList.Items))
	for i := 0; i < len(podList.Items); i++ {
		pods[i] = &podList.Items[i]
	}
	return pods, nil
}
