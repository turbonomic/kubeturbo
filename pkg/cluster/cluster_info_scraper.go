package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"
	osclient "github.com/openshift/client-go/apps/clientset/versioned"
	machinev1beta1api "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	capiclient "github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned"
	policyv1alpha1 "github.com/turbonomic/turbo-policy/api/v1alpha1"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	client "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	commonutil "github.com/turbonomic/kubeturbo/pkg/util"
	gitopsv1alpha1 "github.com/turbonomic/turbo-gitops/api/v1alpha1"
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
	listOptions           = runtimeclient.ListOptions{
		Namespace: api.NamespaceAll,
	}
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
	GetResources(resource schema.GroupVersionResource) ([]unstructured.Unstructured, error)
	GetResourcesPaginated(resource schema.GroupVersionResource, itemsPerPage int) ([]unstructured.Unstructured, error)
	GetMachineSetToNodesMap(nodes []*api.Node) map[string][]*api.Node
	GetAllTurboSLOScalings() ([]policyv1alpha1.SLOHorizontalScale, error)
	GetAllTurboCVSScalings() ([]policyv1alpha1.ContainerVerticalScale, error)
	GetAllTurboPolicyBindings() ([]policyv1alpha1.PolicyBinding, error)
	GetAllGitOpsConfigurations() ([]gitopsv1alpha1.GitOps, error)
	UpdateGitOpsConfigCache()
}

type ClusterScraper struct {
	*client.Clientset
	RestConfig              *restclient.Config
	caClient                *capiclient.Clientset
	capiNamespace           string
	DynamicClient           dynamic.Interface
	OsClient                *osclient.Clientset
	ControllerRuntimeClient runtimeclient.Client
	cache                   turbostore.ITurboCache
	GitOpsConfigCache       map[string][]*gitopsv1alpha1.Configuration
	GitOpsConfigCacheLock   sync.Mutex
}

func NewClusterScraper(restConfig *restclient.Config, kclient *client.Clientset, dynamicClient dynamic.Interface,
	rtClient runtimeclient.Client, osClient *osclient.Clientset, capiEnabled bool, caClient *capiclient.Clientset, capiNamespace string) *ClusterScraper {
	clusterScraper := &ClusterScraper{
		Clientset:               kclient,
		RestConfig:              restConfig,
		DynamicClient:           dynamicClient,
		OsClient:                osClient,
		ControllerRuntimeClient: rtClient,
		// Create cache with expiration duration as defaultCacheTTL, which means the cached data will be cleaned up after
		// defaultCacheTTL.
		cache:             turbostore.NewTurboCache(defaultCacheTTL).Cache,
		GitOpsConfigCache: make(map[string][]*gitopsv1alpha1.Configuration),
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

func (s *ClusterScraper) GetMachineSetToNodesMap(allNodes []*api.Node) map[string][]*api.Node {
	machineSetToNodes := make(map[string][]*api.Node)
	if s.caClient == nil {
		return machineSetToNodes
	}
	// Get the list of machine sets in the cluster
	machineSetList := s.getCApiMachineSets()
	if machineSetList == nil {
		return machineSetToNodes
	}

	for _, machineSet := range machineSetList.Items {
		//Get the list of machines in each machine set
		machines := s.getCApiMachinesFiltered(machineSet.Name, metav1.ListOptions{
			LabelSelector: metav1.FormatLabelSelector(&machineSet.Spec.Selector),
		})

		// from machine -> node
		nodes := getNodesFromMachines(machines, allNodes)
		machineSetToNodes[machineSetPoolName(machineSet.Name)] = nodes
	}
	return machineSetToNodes
}

func getNodesFromMachines(machines []machinev1beta1api.Machine, allNodes []*api.Node) []*api.Node {
	nodes := []*api.Node{}
	for _, machine := range machines {
		if machine.Status.NodeRef != nil && machine.Status.NodeRef.Name != "" {
			if node := findNode(machine.Status.NodeRef.Name, allNodes); node != nil {
				nodes = append(nodes, node)
			}
		}
	}
	return nodes
}

func machineSetPoolName(machineSetName string) string {
	return fmt.Sprintf("%s-%s", machineSetNodePoolPrefix, machineSetName)
}

func findNode(nodeName string, nodes []*api.Node) *api.Node {
	for _, n := range nodes {
		if n.Name == nodeName {
			return n
		}
	}
	return nil
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

func (s *ClusterScraper) GetResources(resource schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	list, err := s.DynamicClient.Resource(resource).Namespace(api.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil || list == nil {
		return nil, err
	}
	return list.Items, err
}

func (s *ClusterScraper) GetResourcesPaginated(
	resource schema.GroupVersionResource, itemsPerPage int) ([]unstructured.Unstructured, error) {
	var items []unstructured.Unstructured
	continueList := ""
	// TODO: Is there a possibility of this loop never exiting?
	// The documentation states that the APIs reliably return correct values for continue
	for {
		listOptions := metav1.ListOptions{Limit: int64(itemsPerPage), Continue: continueList}
		listItems, err := s.DynamicClient.Resource(resource).Namespace(api.NamespaceAll).List(context.TODO(), listOptions)
		if err != nil {
			return items, err
		}
		items = append(items, listItems.Items...)
		if listItems.GetContinue() == "" {
			break
		}
		continueList = listItems.GetContinue()
	}
	return items, nil
}

func (s *ClusterScraper) GetKubernetesServiceID() (svcID string, err error) {
	k8sServiceKey := util.K8sServiceKey(k8sDefaultNamespace, kubernetesServiceName)
	if cachedSvcID, exists := s.cache.Get(k8sServiceKey); exists {
		svcID = cachedSvcID.(string)
		glog.V(4).Infof("Using cached service uid: %s for kubernetes service name: %s", svcID, kubernetesServiceName)
		return
	}
	svc, err := s.CoreV1().Services(k8sDefaultNamespace).Get(context.TODO(), kubernetesServiceName, metav1.GetOptions{})
	if err != nil {
		return
	}
	svcID = string(svc.UID)
	glog.V(4).Infof("No cached service uid value found for kubernetes service name: %s", kubernetesServiceName)
	//setting cache to never expire
	s.cache.Set(k8sServiceKey, svcID, -1)
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
				gpOwnerInfo.Containers = controller.Containers
				s.cache.Set(podControllerInfoKey, gpOwnerInfo, 0)
				added++
				continue
			}
		}
		ownerInfo.Containers = controller.Containers
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
//
//	or if its a "ReplicationController" its parent could be "DeploymentConfig" (as in openshift).
//
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
	canHaveGrandParent := false
	var res schema.GroupVersionResource
	switch ownerInfo.Kind {
	case commonutil.KindReplicationController:
		res = schema.GroupVersionResource{
			Group:    commonutil.K8sAPIReplicationControllerGV.Group,
			Version:  commonutil.K8sAPIReplicationControllerGV.Version,
			Resource: commonutil.ReplicationControllerResName}
		canHaveGrandParent = true
	case commonutil.KindReplicaSet:
		res = schema.GroupVersionResource{
			Group:    commonutil.K8sAPIReplicasetGV.Group,
			Version:  commonutil.K8sAPIReplicasetGV.Version,
			Resource: commonutil.ReplicaSetResName}
		canHaveGrandParent = true
	case commonutil.KindStatefulSet:
		res = schema.GroupVersionResource{
			Group:    commonutil.K8sAPIStatefulsetGV.Group,
			Version:  commonutil.K8sAPIStatefulsetGV.Version,
			Resource: commonutil.StatefulSetResName}
	case commonutil.KindDaemonSet:
		res = schema.GroupVersionResource{
			Group:    commonutil.K8sAPIDaemonsetGV.Group,
			Version:  commonutil.K8sAPIDaemonsetGV.Version,
			Resource: commonutil.DaemonSetResName}
	case commonutil.KindJob:
		res = schema.GroupVersionResource{
			Group:    commonutil.K8sAPIJobGV.Group,
			Version:  commonutil.K8sAPIJobGV.Version,
			Resource: commonutil.JobResName}
	case commonutil.KindCronJob:
		res = schema.GroupVersionResource{
			Group:    commonutil.K8sAPICronJobGV.Group,
			Version:  commonutil.K8sAPICronJobGV.Version,
			Resource: commonutil.CronJobResName}
	default:
		// Unknown resource, we still return the parent if we found one
		s.cache.Set(podControllerInfoKey, ownerInfo, 0)
		return ownerInfo, nil, nil, nil
	}

	var parent *unstructured.Unstructured
	var containerNames sets.String
	namespacedClient := s.DynamicClient.Resource(res).Namespace(pod.Namespace)
	parent, err = namespacedClient.Get(context.TODO(), ownerInfo.Name, metav1.GetOptions{})
	if err != nil {
		return util.OwnerInfo{}, nil, nil, fmt.Errorf("failed to get %s[%v/%v]: %v", ownerInfo.Kind, pod.Namespace, ownerInfo.Name, err)
	}
	containerNames, err = util.GetContainerNames(parent)
	if err != nil {
		return util.OwnerInfo{}, nil, nil, fmt.Errorf("failed to get container names from parent %s[%v/%v]: %v", ownerInfo.Kind, pod.Namespace, ownerInfo.Name, err)
	}

	if canHaveGrandParent {
		//2.2 get parent's parent info by parsing ownerReferences:
		parentOwnerInfo, gpOwnerSet := util.GetOwnerInfo(parent.GetOwnerReferences())
		if gpOwnerSet {
			parentOwnerInfo.Containers = containerNames
			s.cache.Set(podControllerInfoKey, parentOwnerInfo, 0)
			return parentOwnerInfo, parent, namespacedClient, nil
		}
	}

	ownerInfo.Containers = containerNames
	s.cache.Set(podControllerInfoKey, ownerInfo, 0)
	return ownerInfo, parent, namespacedClient, nil
}

// GetAllTurboSLOScalings gets the custom SLOHorizontalScale resource from all namespaces
func (s *ClusterScraper) GetAllTurboSLOScalings() ([]policyv1alpha1.SLOHorizontalScale, error) {
	sloScaleList := &policyv1alpha1.SLOHorizontalScaleList{}
	if err := s.ControllerRuntimeClient.List(context.TODO(), sloScaleList, &listOptions); err != nil {
		return nil, err
	}
	return sloScaleList.Items, nil
}

// GetAllTurboCVSScalings gets the custom ContainerVerticalScale resource from all namespaces
func (s *ClusterScraper) GetAllTurboCVSScalings() ([]policyv1alpha1.ContainerVerticalScale, error) {
	cvsScaleList := &policyv1alpha1.ContainerVerticalScaleList{}
	if err := s.ControllerRuntimeClient.List(context.TODO(), cvsScaleList, &listOptions); err != nil {
		return nil, err
	}
	return cvsScaleList.Items, nil
}

// GetAllTurboPolicyBindings gets the custom PolicyBinding resource from all namespaces
func (s *ClusterScraper) GetAllTurboPolicyBindings() ([]policyv1alpha1.PolicyBinding, error) {
	policyBindingList := &policyv1alpha1.PolicyBindingList{}
	if err := s.ControllerRuntimeClient.List(context.TODO(), policyBindingList, &listOptions); err != nil {
		return nil, err
	}
	return policyBindingList.Items, nil
}

// GetAllGitOpsConfigurations gets the custom GitOps configuration resources from all namespaces
func (s *ClusterScraper) GetAllGitOpsConfigurations() ([]gitopsv1alpha1.GitOps, error) {
	gitopsList := &gitopsv1alpha1.GitOpsList{}
	if err := s.ControllerRuntimeClient.List(context.TODO(), gitopsList, &listOptions); err != nil {
		return nil, err
	}
	return gitopsList.Items, nil
}

func (s *ClusterScraper) UpdateGitOpsConfigCache() {
	configs, err := s.GetAllGitOpsConfigurations()
	if err != nil {
		glog.V(2).Infof("Failed to discover GitOps configurations: %v", err)
		return
	}
	gitOpsConfigCache := make(map[string][]*gitopsv1alpha1.Configuration)
	for _, gitOpsConfig := range configs {
		namespace := gitOpsConfig.GetNamespace()
		for _, c := range gitOpsConfig.Spec.Configuration {
			gitOpsConfigCache[namespace] = append(gitOpsConfigCache[namespace], &c)
		}
	}
	// Lock the "cache" to prevent access while it gets overwritten
	s.GitOpsConfigCacheLock.Lock()
	defer s.GitOpsConfigCacheLock.Unlock()
	s.GitOpsConfigCache = gitOpsConfigCache
}
