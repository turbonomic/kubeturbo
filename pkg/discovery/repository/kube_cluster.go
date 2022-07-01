package repository

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-crd/api/v1alpha1"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// KubeCluster defines the Kubernetes cluster. This object is immutable between discoveries.
// New discovery will bring in changes to the nodes and namespaces
// Aggregate structure for nodes, namespaces and quotas
type KubeCluster struct {
	Name         string
	Nodes        []*v1.Node
	Pods         []*v1.Pod
	NamespaceMap map[string]*KubeNamespace
	// Map of Service to Pod cluster Ids
	Services map[*v1.Service][]string
	// Map of Persistent Volumes to namespace qualified pod names with their
	// volume names (as named in podSpec).
	// The unused PV will have the slice value set to nil.
	VolumeToPodsMap map[*v1.PersistentVolume][]PodVolume
	// Map of namespace qualified pod name wrt to the volumes they mount.
	// This map will not feature volumes which are not mounted by any pods.
	PodToVolumesMap map[string][]MountedVolume

	K8sAppToComponentMap map[K8sApp][]K8sAppComponent
	ComponentToAppMap    map[K8sAppComponent][]K8sApp
	ControllerMap        map[string]*K8sController

	// Map listing parent machineSet name for each nodename
	// This will be filled only if openshift clusterapi is enabled
	MachineSetToNodeUIDsMap map[string][]string

	// Data structures related to Turbo policy
	TurboPolicyBindings []*TurboPolicyBinding
}

func NewKubeCluster(clusterName string, nodes []*v1.Node) *KubeCluster {
	return &KubeCluster{
		Name:         clusterName,
		Nodes:        nodes,
		Pods:         []*v1.Pod{},
		NamespaceMap: make(map[string]*KubeNamespace),
	}
}

func (kc *KubeCluster) WithPods(pods []*v1.Pod) *KubeCluster {
	kc.Pods = pods
	return kc
}

func (kc *KubeCluster) WithMachineSetToNodeUIDsMap(machineSetToNodeUIDsMap map[string][]string) *KubeCluster {
	kc.MachineSetToNodeUIDsMap = machineSetToNodeUIDsMap
	return kc
}

// ClusterSummary defines a summary object to get the nodes, quotas and namespaces in the cluster
type ClusterSummary struct {
	*KubeCluster
	// Computed
	NodeMap                  map[string]*KubeNode
	NodeNameUIDMap           map[string]string
	NamespaceUIDMap          map[string]string
	PodClusterIDToServiceMap map[string]*v1.Service
	// Map node to all Running pods on the node.
	// Pod in Running phase may NOT necessarily be in a Ready condition
	NodeToRunningPods map[string][]*v1.Pod
	// Map node to all Pending pods on the node
	// A scheduled pod can still be in Pending phase, for example, when
	// it fails to pull image onto the host
	NodeToPendingPods       map[string][]*v1.Pod
	AverageNodeCpuFrequency float64
}

func CreateClusterSummary(kubeCluster *KubeCluster) *ClusterSummary {
	clusterSummary := &ClusterSummary{
		KubeCluster:              kubeCluster,
		NodeMap:                  make(map[string]*KubeNode),
		NodeNameUIDMap:           make(map[string]string),
		NamespaceUIDMap:          make(map[string]string),
		PodClusterIDToServiceMap: make(map[string]*v1.Service),
		NodeToRunningPods:        make(map[string][]*v1.Pod),
		NodeToPendingPods:        make(map[string][]*v1.Pod),
	}
	clusterSummary.computeNodeMap()
	clusterSummary.computePodMap()
	clusterSummary.computeQuotaMap()
	clusterSummary.computePodToServiceMap()
	return clusterSummary
}

func (summary *ClusterSummary) GetKubeNamespace(namespace string) *KubeNamespace {
	kubeNamespace, exists := summary.NamespaceMap[namespace]
	if !exists {
		glog.Errorf("Cannot find KubeNamespace entity for namespace %s", namespace)
		return nil
	}
	return kubeNamespace
}

func (summary *ClusterSummary) GetNodeCPUFrequency(nodeName string) float64 {
	node, exists := summary.NodeMap[nodeName]
	if !exists {
		glog.Errorf("Cannot find KubeNamespace entity for namespace %s", nodeName)
		return 0
	}
	return node.NodeCpuFrequency
}

func (summary *ClusterSummary) GetReadyPods() (readyPods []*v1.Pod) {
	for _, pod := range summary.Pods {
		if util.PodIsReady(pod) {
			readyPods = append(readyPods, pod)
		}
	}
	return
}

func (summary *ClusterSummary) GetRunningPodsOnNode(node *v1.Node) []*v1.Pod {
	runningPods, exists := summary.NodeToRunningPods[node.Name]
	if !exists {
		glog.V(3).Infof("No running pods found on node %v.", node.Name)
		return nil
	}
	return runningPods

}

func (summary *ClusterSummary) GetPendingPodsOnNode(node *v1.Node) []*v1.Pod {
	pendingPods, exists := summary.NodeToPendingPods[node.Name]
	if !exists {
		glog.V(3).Infof("No pending pods found on node %v.", node.Name)
		return nil
	}
	return pendingPods

}

func (summary *ClusterSummary) computePodMap() {
	for _, pod := range summary.Pods {
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			glog.V(3).Infof("Skip pod %v/%v without assigned node.",
				pod.Namespace, pod.Name)
			continue
		}
		switch pod.Status.Phase {
		case v1.PodPending:
			summary.NodeToPendingPods[nodeName] =
				append(summary.NodeToPendingPods[nodeName], pod)
		case v1.PodRunning:
			summary.NodeToRunningPods[nodeName] =
				append(summary.NodeToRunningPods[nodeName], pod)
		default:
			glog.V(3).Infof("Skip pod %v/%v with status %v.",
				pod.Namespace, pod.Name, pod.Status.Phase)
		}
	}
}

func (summary *ClusterSummary) computeNodeMap() {
	for _, node := range summary.Nodes {
		summary.NodeMap[node.Name] = NewKubeNode(node, summary.Name)
		summary.NodeNameUIDMap[node.Name] = string(node.UID)
	}
	return
}

func (summary *ClusterSummary) computeQuotaMap() {
	for namespaceName, namespace := range summary.NamespaceMap {
		summary.NamespaceUIDMap[namespaceName] = namespace.UID
	}
	return
}

func (summary *ClusterSummary) computePodToServiceMap() {
	for svc, podList := range summary.Services {
		for _, podClusterID := range podList {
			summary.PodClusterIDToServiceMap[podClusterID] = svc
		}
	}
}

const (
	DEFAULT_METRIC_VALUE          float64 = 0.0
	DEFAULT_METRIC_CAPACITY_VALUE         = 1e12
)

// KubeNode defines the node in the cluster
type KubeNode struct {
	*KubeEntity
	*v1.Node

	// node properties
	NodeCpuFrequency float64 // Set by the metrics collector which processes this node
}

// NewKubeNode create a KubeNode entity with compute resources to represent a node in the cluster
func NewKubeNode(apiNode *v1.Node, clusterName string) *KubeNode {
	entity := NewKubeEntity(metrics.NodeType, clusterName,
		apiNode.ObjectMeta.Namespace, apiNode.ObjectMeta.Name,
		string(apiNode.ObjectMeta.UID))

	nodeEntity := &KubeNode{
		KubeEntity: entity,
		Node:       apiNode,
	}

	// Node compute resources and properties
	nodeEntity.updateResources(apiNode)
	return nodeEntity
}

// Parse the api node object to set the compute resources and IP property in the node entity
func (nodeEntity *KubeNode) updateResources(apiNode *v1.Node) {
	// Node compute resources
	resourceAllocatableList := apiNode.Status.Allocatable
	for resource := range resourceAllocatableList {
		computeResourceTypeList, isComputeType := metrics.KubeComputeResourceTypes[resource]
		if !isComputeType {
			continue
		}
		for _, computeResourceType := range computeResourceTypeList {
			capacityValue := parseResourceValue(computeResourceType, resourceAllocatableList)
			nodeEntity.AddComputeResource(computeResourceType, float64(capacityValue), DEFAULT_METRIC_VALUE)
		}
	}
}

// ParseNodeIP parses the Node instances returned by the kubernetes API to get the IP address
func ParseNodeIP(apiNode *v1.Node, addressType v1.NodeAddressType) string {
	nodeAddresses := apiNode.Status.Addresses
	for _, nodeAddress := range nodeAddresses {
		if nodeAddress.Type == addressType && nodeAddress.Address != "" {
			glog.V(4).Infof("%s : %s is %s", apiNode.Name, addressType, nodeAddress.Address)
			return nodeAddress.Address
		}
	}
	return ""
}

func (nodeEntity *KubeNode) String() string {
	var buffer bytes.Buffer
	buffer.WriteString(nodeEntity.KubeEntity.String())
	line := fmt.Sprintf("SystemUUID:%s\n", nodeEntity.Status.NodeInfo.SystemUUID)
	buffer.WriteString(line)
	line = fmt.Sprintf("CPU Frequency:%f\n", nodeEntity.NodeCpuFrequency)
	buffer.WriteString(line)
	return buffer.String()
}

func parseResourceValue(computeResourceType metrics.ResourceType, resourceList v1.ResourceList) float64 {
	if computeResourceType == metrics.CPU ||
		computeResourceType == metrics.CPURequest {
		ctnCpuCapacityMilliCore := resourceList.Cpu().MilliValue()
		return float64(ctnCpuCapacityMilliCore)
	}

	if computeResourceType == metrics.Memory ||
		computeResourceType == metrics.MemoryRequest {
		ctnMemoryCapacityBytes := resourceList.Memory().Value()
		memoryCapacityKiloBytes := util.Base2BytesToKilobytes(float64(ctnMemoryCapacityBytes))
		return memoryCapacityKiloBytes
	}
	return DEFAULT_METRIC_VALUE
}

const (
	AppTypeK8s    = "k8s"
	AppTypeArgoCD = "argocd"
)

type K8sApp struct {
	Uid       string
	Namespace string
	Name      string
	Type      string
}

type K8sAppComponent struct {
	EntityType proto.EntityDTO_EntityType
	Uid        string
	Namespace  string
	Name       string
}

type PodVolume struct {
	// Namespace qualified pod name.
	QualifiedPodName string
	// Name used by the pod to mount the volume.
	MountName string
}

type MountedVolume struct {
	UsedVolume *v1.PersistentVolume
	MountName  string
}

// PodVolumeMetrics defines volume metrics reported for a given pod
type PodVolumeMetrics struct {
	Volume   *v1.PersistentVolume
	Capacity float64
	Used     float64
	PodVolume
}

// K8sController defins an immutable snapshot of a k8s workload controller.
// This is used by ControllerProcessor to temporarily cache the critical information of a k8s controller
// for efficient lookup.
// This is different from the KubeController object which can be incrementally updated or
// aggregated during discovery to fill in list of pods, resource usage, etc.
type K8sController struct {
	Kind            string
	Name            string
	Namespace       string
	UID             string
	Labels          map[string]string
	Annotations     map[string]string
	OwnerReferences []metav1.OwnerReference
	// May not exist in all controllers. For Daemonset, defaults to number of nodes in the cluster.
	Replicas   *int64
	Containers sets.String
}

func NewK8sController(kind, name, namespace, uid string) *K8sController {
	return &K8sController{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
		UID:       uid,
	}
}

func (kc *K8sController) WithLabels(labels map[string]string) *K8sController {
	kc.Labels = labels
	return kc
}

func (kc *K8sController) WithAnnotations(annotations map[string]string) *K8sController {
	kc.Annotations = annotations
	return kc
}

func (kc *K8sController) WithOwnerReferences(owners []metav1.OwnerReference) *K8sController {
	kc.OwnerReferences = owners
	return kc
}

func (kc *K8sController) WithReplicas(replicas int64) *K8sController {
	kc.Replicas = &replicas
	return kc
}

func (kc *K8sController) WithContainerNames(names sets.String) *K8sController {
	kc.Containers = names
	return kc
}

// KubeController defines K8s controller in the cluster
type KubeController struct {
	*KubeEntity
	ControllerType string
	Pods           []*v1.Pod
}

func NewKubeController(clusterName, namespace, name, controllerType, uid string) *KubeController {
	entity := NewKubeEntity(metrics.ControllerType, clusterName, namespace, name, uid)
	return &KubeController{
		KubeEntity:     entity,
		ControllerType: controllerType,
		Pods:           []*v1.Pod{},
	}
}

// GetFullName constructs controller full name by: "namespace/controllerType/controllerName"
func (kubeController *KubeController) GetFullName() string {
	return kubeController.Namespace + "/" + kubeController.ControllerType + "/" + kubeController.Name
}

func (kubeController *KubeController) String() string {
	var buffer bytes.Buffer
	line := fmt.Sprintf("K8s controller: %s\n", kubeController.GetFullName())
	buffer.WriteString(line)
	buffer.WriteString(kubeController.KubeEntity.String())
	return buffer.String()
}

// KubeNamespace defines the namespace in the cluster
type KubeNamespace struct {
	*KubeEntity
	// List of quotas defined for a namespace
	QuotaList               []*v1.ResourceQuota
	AverageNodeCpuFrequency float64
	QuotaDefined            map[metrics.ResourceType]bool

	// Stores the labels and annotations on the given namespace
	Labels      map[string]string
	Annotations map[string]string
}

func (kubeNamespace *KubeNamespace) String() string {
	var buffer bytes.Buffer
	line := fmt.Sprintf("Namespace:%s\n", kubeNamespace.Name)
	buffer.WriteString(line)
	if len(kubeNamespace.QuotaList) > 0 {
		var quotaNames []string
		for _, quota := range kubeNamespace.QuotaList {
			quotaNames = append(quotaNames, quota.Name)
		}
		quotaStr := strings.Join(quotaNames, ",")
		line = fmt.Sprintf("Contains resource quota(s) : %s\n", quotaStr)
		buffer.WriteString(line)
	}

	var prefix string
	if len(kubeNamespace.QuotaList) == 0 {
		prefix = "Default "
	} else {
		prefix = "Reconciled "
	}
	buffer.WriteString(prefix)
	buffer.WriteString(kubeNamespace.KubeEntity.String())

	return buffer.String()
}

// CreateDefaultKubeNamespace creates default KubeNamespace object.
// Set default resource quota limits and requests capacity to infinity (1e9) because when quota is not configured on namespace
// we want quota commodities sold by namespace have least impact on Market analysis results of containers and pods.
// Will update resource quota capacity when calling ReconcileQuotas func if ResourceQuota is configured on the namespace.
func CreateDefaultKubeNamespace(clusterName, namespace, uuid string) *KubeNamespace {
	kubeNamespace := &KubeNamespace{
		KubeEntity: NewKubeEntity(metrics.NamespaceType, clusterName,
			namespace, namespace, uuid),
		QuotaList:    []*v1.ResourceQuota{},
		QuotaDefined: make(map[metrics.ResourceType]bool),
	}

	// create quota allocation resources
	for _, rt := range metrics.QuotaResources {
		kubeNamespace.AddAllocationResource(rt, DEFAULT_METRIC_CAPACITY_VALUE, DEFAULT_METRIC_VALUE)
	}
	for _, rt := range metrics.ComputeResources {
		kubeNamespace.AddComputeResource(rt, DEFAULT_METRIC_CAPACITY_VALUE, DEFAULT_METRIC_VALUE)
	}
	return kubeNamespace
}

// ReconcileQuotas combinds the quota limits for various resource quota objects defined in the namespace.
// Multiple quota limits for the same resource, if any, are reconciled by selecting the
// most restrictive limit value.
func (kubeNamespace *KubeNamespace) ReconcileQuotas(quotas []*v1.ResourceQuota) {
	// Quota resources by collecting resources from the list of resource quota objects
	var quotaNames []string
	for _, item := range quotas {
		//quotaListStr = quotaListStr + item.Name + ","
		quotaNames = append(quotaNames, item.Name)
		// Resources in each quota
		resourceStatus := item.Status
		resourceHardList := resourceStatus.Hard

		for resource := range resourceHardList {
			// This will return resourceType as "limits.cpu" for resource as both "cpu" and "limits.cpu"
			// Likewise for memory.
			resourceType, isAllocationType := metrics.KubeQuotaResourceTypes[resource]
			if !isAllocationType { // skip if it is not a allocation type resource
				continue
			}
			// Quota is defined for the quota resource
			kubeNamespace.QuotaDefined[resourceType] = true

			// Parse the CPU  values into number of millicores, Memory values into KBytes
			capacityValue := parseAllocationResourceValue(resource, resourceType, resourceHardList)

			// Update the namespace entity resource with the most restrictive quota value
			existingResource, err := kubeNamespace.GetAllocationResource(resourceType)
			if err == nil {
				if existingResource.Capacity == DEFAULT_METRIC_VALUE || capacityValue < existingResource.Capacity {
					existingResource.Capacity = capacityValue
				}
			} else {
				// Create resource if it does not exist
				// Used value will be updated in namespace_discovery_worker which is the sum of limits/request from all
				// containers running on this namespace
				kubeNamespace.AddAllocationResource(resourceType, capacityValue, DEFAULT_METRIC_VALUE)
			}
		}
	}
	quotaListStr := strings.Join(quotaNames, ",")
	glog.V(4).Infof("Reconciled Namespace %s from resource quota(s) %s"+
		"[CPULimitQuota:%t, MemoryLimitQuota:%t CPURequestQuota:%t, MemoryRequestQuota:%t]",
		kubeNamespace.Name, quotaListStr,
		kubeNamespace.QuotaDefined[metrics.CPULimitQuota],
		kubeNamespace.QuotaDefined[metrics.MemoryLimitQuota],
		kubeNamespace.QuotaDefined[metrics.CPURequestQuota],
		kubeNamespace.QuotaDefined[metrics.MemoryRequestQuota])
}

// Parse the CPU and Memory resource values.
// CPU is represented in number of millicores, Memory in KBytes
func parseAllocationResourceValue(resource v1.ResourceName, allocationResourceType metrics.ResourceType, resourceList v1.ResourceList) float64 {

	if allocationResourceType == metrics.CPULimitQuota || allocationResourceType == metrics.CPURequestQuota {
		quantity := resourceList[resource]
		cpuMilliCore := quantity.MilliValue()
		return float64(cpuMilliCore)
	}

	if allocationResourceType == metrics.MemoryLimitQuota || allocationResourceType == metrics.MemoryRequestQuota {
		quantity := resourceList[resource]
		memoryBytes := quantity.Value()
		memoryKiloBytes := util.Base2BytesToKilobytes(float64(memoryBytes))
		return memoryKiloBytes
	}
	return DEFAULT_METRIC_VALUE
}

type TurboPolicy struct {
	SLOHorizontalScale *v1alpha1.SLOHorizontalScale
}

func NewTurboPolicy() *TurboPolicy {
	return &TurboPolicy{}
}

func (p *TurboPolicy) WithSLOHorizontalScale(policy *v1alpha1.SLOHorizontalScale) *TurboPolicy {
	p.SLOHorizontalScale = policy
	return p
}

type TurboPolicyBinding struct {
	PolicyBinding *v1alpha1.PolicyBinding
	TurboPolicy
}

func NewTurboPolicyBinding(policyBinding *v1alpha1.PolicyBinding) *TurboPolicyBinding {
	return &TurboPolicyBinding{
		PolicyBinding: policyBinding,
	}
}

func (b *TurboPolicyBinding) WithTurboPolicy(policy *TurboPolicy) *TurboPolicyBinding {
	b.TurboPolicy = *policy
	return b
}

func (b *TurboPolicyBinding) GetUID() string {
	return string(b.PolicyBinding.GetUID())
}

func (b *TurboPolicyBinding) GetNamespace() string {
	return b.PolicyBinding.GetNamespace()
}

func (b *TurboPolicyBinding) GetName() string {
	return b.PolicyBinding.GetName()
}

func (b *TurboPolicyBinding) GetPolicyType() string {
	return b.PolicyBinding.Spec.PolicyRef.Kind
}

func (b *TurboPolicyBinding) GetSLOHorizontalScaleSpec() *v1alpha1.SLOHorizontalScaleSpec {
	if b.SLOHorizontalScale == nil {
		return nil
	}
	return &b.SLOHorizontalScale.Spec
}

func (b *TurboPolicyBinding) GetTargets() []v1alpha1.PolicyTargetReference {
	return b.PolicyBinding.Spec.Targets
}

func (b *TurboPolicyBinding) String() string {
	return b.GetNamespace() + "/" + b.GetName()
}
