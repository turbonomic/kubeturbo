package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"

	machinev1beta1api "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	capiclient "github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	client "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	commonutil "github.com/turbonomic/kubeturbo/pkg/util"
)

const (
	k8sDefaultNamespace      = "default"
	kubernetesServiceName    = "kubernetes"
	machineSetNodePoolPrefix = "machineset"
	// Expiration of cached pod controller info.
	defaultCacheTTL = 12 * time.Hour
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
	GetResources(resource schema.GroupVersionResource) (*unstructured.UnstructuredList, error)
	GetMachineSetToNodeUIDsMap(nodes []*api.Node) map[string][]string
}

type ClusterScraper struct {
	*client.Clientset
	caClient      *capiclient.Clientset
	capiNamespace string
	DynamicClient dynamic.Interface
	cache         turbostore.ITurboCache
}

func NewClusterScraper(kclient *client.Clientset, dynamicClient dynamic.Interface, capiEnabled bool,
	caClient *capiclient.Clientset, capiNamespace string) *ClusterScraper {
	clusterScraper := &ClusterScraper{
		Clientset:     kclient,
		DynamicClient: dynamicClient,
		// Create cache with expiration duration as defaultCacheTTL, which means the cached data will be cleaned up after
		// defaultCacheTTL.
		cache: turbostore.NewTurboCache(defaultCacheTTL).Cache,
	}

	if capiEnabled {
		clusterScraper.caClient = caClient
		clusterScraper.capiNamespace = capiNamespace
	}
	return clusterScraper
}

func (s *ClusterScraper) GetNamespaces() ([]*api.Namespace, error) {
	namespaceList, err := s.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
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
	quotaList, err := s.CoreV1().ResourceQuotas(namespace).List(context.TODO(), metav1.ListOptions{})
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

func (s *ClusterScraper) GetMachineSetToNodeUIDsMap(nodes []*api.Node) map[string][]string {
	machineSetToNodeUIDs := make(map[string][]string)
	if s.caClient == nil {
		return machineSetToNodeUIDs
	}

	machineSetList := s.getCApiMachineSets()
	if machineSetList == nil {
		return machineSetToNodeUIDs
	}

	for _, machineSet := range machineSetList.Items {
		machines := s.getCApiMachinesFiltered(machineSet.Name, metav1.ListOptions{
			LabelSelector: metav1.FormatLabelSelector(&machineSet.Spec.Selector),
		})
		nodeUIDs := []string{}
		for _, machine := range machines {
			if machine.Status.NodeRef != nil && machine.Status.NodeRef.Name != "" {
				if uid := findNodeUID(machine.Status.NodeRef.Name, nodes); uid != "" {
					nodeUIDs = append(nodeUIDs, uid)
				}
			}
		}
		machineSetToNodeUIDs[machineSetPoolName(machineSet.Name)] = nodeUIDs
	}
	return machineSetToNodeUIDs
}

func machineSetPoolName(machineSetName string) string {
	return fmt.Sprintf("%s-%s", machineSetNodePoolPrefix, machineSetName)
}

func findNodeUID(nodeName string, nodes []*api.Node) string {
	uid := ""
	for _, n := range nodes {
		if n.Name == nodeName {
			return string(n.UID)
		}
	}
	return uid
}

func (s *ClusterScraper) getCApiMachineSets() *machinev1beta1api.MachineSetList {
	machineSetList, err := s.caClient.MachineV1beta1().MachineSets(s.capiNamespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		glog.Errorf("Error retrieving machinesets from cluster: %v", err)
		return nil
	}

	return machineSetList
}

func (s *ClusterScraper) getCApiMachinesFiltered(machinesetName string, listOptions metav1.ListOptions) []machinev1beta1api.Machine {
	machineList, err := s.caClient.MachineV1beta1().Machines(s.capiNamespace).List(context.TODO(), listOptions)
	if err != nil {
		glog.Errorf("Error retrieving machines for machineset %s from cluster: %v", machinesetName, err)
		return nil
	}
	var machines []machinev1beta1api.Machine
	if machineList != nil {
		machines = append(machines, machineList.Items...)
	}
	return machines
}

func (s *ClusterScraper) GetNodes(opts metav1.ListOptions) ([]*api.Node, error) {
	nodeList, err := s.CoreV1().Nodes().List(context.TODO(), opts)
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
	podList, err := s.CoreV1().Pods(namespaces).List(context.TODO(), opts)
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
	serviceList, err := s.CoreV1().Services(namespace).List(context.TODO(), opts)
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
	epList, err := s.CoreV1().Endpoints(namespaces).List(context.TODO(), opts)
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

func (s *ClusterScraper) GetResources(resource schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	return s.DynamicClient.Resource(resource).Namespace(api.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
}

func (s *ClusterScraper) GetKubernetesServiceID() (svcID string, err error) {
	svc, err := s.CoreV1().Services(k8sDefaultNamespace).Get(context.TODO(), kubernetesServiceName, metav1.GetOptions{})
	if err != nil {
		return
	}
	svcID = string(svc.UID)
	return
}

func (s *ClusterScraper) GetAllPVs() ([]*api.PersistentVolume, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labelSelectEverything,
	}

	pvList, err := s.CoreV1().PersistentVolumes().List(context.TODO(), listOption)
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
	pvcList, err := s.CoreV1().PersistentVolumeClaims(namespace).List(context.TODO(), opts)
	if err != nil {
		return nil, err
	}

	pvcs := make([]*api.PersistentVolumeClaim, len(pvcList.Items))
	for i := 0; i < len(pvcList.Items); i++ {
		pvcs[i] = &pvcList.Items[i]
	}
	return pvcs, nil
}

// UpdatePodControllerCache is called at the beginning of a full discovery after all pods and a predefined types of
// workload controllers (see processor.ControllerProcessor for details) are discovered. We tried to cache all pods'
// controller info (i.e., kind, name, uid) into the cache maintained by ClusterScraper for speedy look up when building
// entity DTOs.
// A pod can have its labels changed on the fly without being restarted, causing its controller to be changed as well.
// This is very unlikely but still possible. This can be detected when the cache entry expires in defaultCacheTTL.
func (s *ClusterScraper) UpdatePodControllerCache(
	pods []*api.Pod, controllers map[string]*repository.K8sController) {
	existing := 0
	added := 0
	bare := 0
	custom := 0
	for _, pod := range pods {
		podControllerInfoKey := util.PodControllerInfoKey(pod)
		if _, exists := s.cache.Get(podControllerInfoKey); exists {
			// Pod's controller info already exists in the cache
			existing++
			continue
		}
		ownerInfo, err := util.GetPodParentInfo(pod)
		if err != nil || util.IsOwnerInfoEmpty(ownerInfo) {
			// Pod does not have controller
			glog.V(3).Infof("Skip updating controller for pod %v/%v: pod has no controller.",
				pod.Namespace, pod.Name)
			bare++
			continue
		}
		controller, exists := controllers[ownerInfo.Uid]
		if !exists {
			// Could be custom controller. We do not bulk process custom controller.
			glog.V(3).Infof("Skip updating controller %v/%v for pod %v/%v: controller not cached.",
				ownerInfo.Kind, ownerInfo.Name, pod.Namespace, pod.Name)
			custom++
			continue
		}
		kind := controller.Kind
		if kind == commonutil.KindReplicationController || kind == commonutil.KindReplicaSet {
			// Get grandparent of pod
			gpOwnerInfo, gpOwnerSet := util.GetOwnerInfo(controller.OwnerReferences)
			if gpOwnerSet {
				// Found it
				s.cache.Set(podControllerInfoKey, gpOwnerInfo, 0)
				added++
				continue
			}
		}
		s.cache.Set(podControllerInfoKey, ownerInfo, 0)
		added++
	}
	glog.V(2).Infof("Finished updating pod controller cache."+
		" Total pod scanned: %d, cached: %d, newly added: %d, bare pods: %d, pods with custom controllers: %d",
		len(pods), existing, added, bare, custom)
}

// GetPodControllerInfo gets grandParent (parent's parent) information of a pod: kind, name, uid
// If parent does not have parent, then return parent info.
// Note: if parent kind is "ReplicaSet", then its parent's parent can be a "Deployment"
//       or if its a "ReplicationController" its parent could be "DeploymentConfig" (as in openshift).
// The function also returns the retrieved parent and parents crud interface for use by the callers.
// If ignoreCache is set to false, this function will first try to get pod's controller info from the cache
// maintained by the ClusterScraper.
func (s *ClusterScraper) GetPodControllerInfo(
	pod *api.Pod, useCache bool) (util.OwnerInfo, *unstructured.Unstructured, dynamic.ResourceInterface, error) {
	podControllerInfoKey := util.PodControllerInfoKey(pod)
	if useCache {
		// Get pod controller info from cache if exists
		controllerInfoCache, exists := s.cache.Get(podControllerInfoKey)
		if exists {
			if ownerInfo, ok := controllerInfoCache.(util.OwnerInfo); ok {
				return ownerInfo, nil, nil, nil
			}
			glog.Warningf("Failed to get controller info cache data: controllerInfoCache is '%t' not 'OwnerInfo'",
				controllerInfoCache)
		}
	}

	//1. get Parent info: kind and name;
	ownerInfo, err := util.GetPodParentInfo(pod)
	if err != nil {
		// Pod does not have controller
		return util.OwnerInfo{}, nil, nil, err
	}

	//2. if parent is "ReplicaSet" or "ReplicationController", check parent's parent
	var res schema.GroupVersionResource
	switch ownerInfo.Kind {
	case commonutil.KindReplicationController:
		res = schema.GroupVersionResource{
			Group:    commonutil.K8sAPIReplicationControllerGV.Group,
			Version:  commonutil.K8sAPIReplicationControllerGV.Version,
			Resource: commonutil.ReplicationControllerResName}
	case commonutil.KindReplicaSet:
		res = schema.GroupVersionResource{
			Group:    commonutil.K8sAPIReplicasetGV.Group,
			Version:  commonutil.K8sAPIReplicasetGV.Version,
			Resource: commonutil.ReplicaSetResName}
	default:
		s.cache.Set(podControllerInfoKey, ownerInfo, 0)
		return ownerInfo, nil, nil, nil
	}

	namespacedClient := s.DynamicClient.Resource(res).Namespace(pod.Namespace)
	obj, err := namespacedClient.Get(context.TODO(), ownerInfo.Name, metav1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("failed to get %s[%v/%v]: %v", ownerInfo.Kind, pod.Namespace, ownerInfo.Name, err)
		glog.Error(err.Error())
		return util.OwnerInfo{}, nil, nil, err
	}
	//2.2 get parent's parent info by parsing ownerReferences:
	gpOwnerInfo, gpOwnerSet := util.GetOwnerInfo(obj.GetOwnerReferences())
	if gpOwnerSet {
		s.cache.Set(podControllerInfoKey, gpOwnerInfo, 0)
		return gpOwnerInfo, obj, namespacedClient, nil
	}
	s.cache.Set(podControllerInfoKey, ownerInfo, 0)
	return ownerInfo, obj, namespacedClient, nil
}
