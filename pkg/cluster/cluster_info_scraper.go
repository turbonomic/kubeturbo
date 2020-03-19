package cluster

import (
	"fmt"

	"k8s.io/client-go/dynamic"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	client "k8s.io/client-go/kubernetes"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
)

const (
	k8sDefaultNamespace   = "default"
	kubernetesServiceName = "kubernetes"
)

var (
	labelSelectEverything = labels.Everything().String()
	fieldSelectEverything = fields.Everything().String()
)

type ClusterScraperInterface interface {
	GetAllNodes() ([]*api.Node, error)
	GetNamespaces() ([]*api.Namespace, error)
	GetNamespaceQuotas() (map[string][]*api.ResourceQuota, error)
	GetAllPods() ([]*api.Pod, error)
	GetAllEndpoints() ([]*api.Endpoints, error)
	GetAllServices() ([]*api.Service, error)
	GetKubernetesServiceID() (svcID string, err error)
	GetAllPVs() ([]*api.PersistentVolume, error)
	GetAllPVCs() ([]*api.PersistentVolumeClaim, error)
}

type ClusterScraper struct {
	*client.Clientset
	DynamicClient dynamic.Interface
}

func NewClusterScraper(kclient *client.Clientset, dynamicClient dynamic.Interface) *ClusterScraper {
	return &ClusterScraper{
		Clientset:     kclient,
		DynamicClient: dynamicClient,
	}
}

func (s *ClusterScraper) GetNamespaces() ([]*api.Namespace, error) {
	namespaceList, err := s.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	namespaces := make([]*api.Namespace, len(namespaceList.Items))
	for i := 0; i < len(namespaceList.Items); i++ {
		namespaces[i] = &namespaceList.Items[i]
	}
	return namespaces, nil
}

func (s *ClusterScraper) getResourceQuotas() ([]*api.ResourceQuota, error) {
	namespace := api.NamespaceAll
	quotaList, err := s.CoreV1().ResourceQuotas(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	quotas := make([]*api.ResourceQuota, len(quotaList.Items))
	for i := 0; i < len(quotaList.Items); i++ {
		quotas[i] = &quotaList.Items[i]
	}
	return quotas, nil
}

// Return a map containing namespace and the list of quotas defined in the namespace.
func (s *ClusterScraper) GetNamespaceQuotas() (map[string][]*api.ResourceQuota, error) {
	quotaList, err := s.getResourceQuotas()
	if err != nil {
		return nil, err
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
	n := len(nodeList.Items)
	nodes := make([]*api.Node, n)
	for i := 0; i < n; i++ {
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

func (s *ClusterScraper) GetRunningAndReadyPodsOnNode(node *api.Node) []*api.Pod {
	nodeRunningPodsList, err := s.findRunningPodsOnNode(node.Name)
	if err != nil {
		glog.Errorf("Failed to find running pods in %s", node.Name)
		return []*api.Pod{}
	}

	return util.GetReadyPods(nodeRunningPodsList)
}

func (s *ClusterScraper) GetRunningAndReadyPodsOnNodes(nodeList []*api.Node) []*api.Pod {
	pods := []*api.Pod{}
	for _, node := range nodeList {
		nodeRunningPodsList, err := s.findRunningPodsOnNode(node.Name)
		if err != nil {
			glog.Errorf("Failed to find running pods in %s", node.Name)
			continue
		}
		pods = append(pods, nodeRunningPodsList...)
	}
	return util.GetReadyPods(pods)
}

// TODO, create a local pod, node cache to avoid too many API request.
func (s *ClusterScraper) findRunningPodsOnNode(nodeName string) ([]*api.Pod, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName + ",status.phase=" +
		string(api.PodRunning))
	if err != nil {
		return nil, err
	}
	podList, err := s.CoreV1().Pods(api.NamespaceAll).List(metav1.ListOptions{FieldSelector: fieldSelector.String()})
	if err != nil {
		return nil, err
	}
	pods := make([]*api.Pod, len(podList.Items))
	for i := 0; i < len(podList.Items); i++ {
		pods[i] = &podList.Items[i]
	}
	return pods, nil
}

func (s *ClusterScraper) GetAllPVs() ([]*api.PersistentVolume, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labelSelectEverything,
	}

	pvList, err := s.CoreV1().PersistentVolumes().List(listOption)
	if err != nil {
		return nil, err
	}

	pvs := make([]*api.PersistentVolume, len(pvList.Items))
	for i := 0; i < len(pvList.Items); i++ {
		pvs[i] = &pvList.Items[i]
	}
	return pvs, nil
}

func (s *ClusterScraper) GetAllPVCs() ([]*api.PersistentVolumeClaim, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labelSelectEverything,
	}

	return s.GetPVCs(api.NamespaceAll, listOption)
}

func (s *ClusterScraper) GetPVCs(namespace string, opts metav1.ListOptions) ([]*api.PersistentVolumeClaim, error) {
	pvcList, err := s.CoreV1().PersistentVolumeClaims(namespace).List(opts)
	if err != nil {
		return nil, err
	}

	pvcs := make([]*api.PersistentVolumeClaim, len(pvcList.Items))
	for i := 0; i < len(pvcList.Items); i++ {
		pvcs[i] = &pvcList.Items[i]
	}
	return pvcs, nil
}
