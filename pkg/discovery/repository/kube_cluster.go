package repository

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	v1 "k8s.io/api/core/v1"
)

// Kube Cluster represents the Kubernetes cluster. This object is immutable between discoveries.
// New discovery will bring in changes to the nodes and namespaces
// Aggregate structure for nodes, namespaces and quotas
type KubeCluster struct {
	Name             string
	Nodes            map[string]*KubeNode
	Namespaces       map[string]*KubeNamespace
	ClusterResources map[metrics.ResourceType]*KubeDiscoveredResource
	// Map of Service to Pod cluster Ids
	Services map[*v1.Service][]string
	// Map of Persistent Volumes to namespace qualified pod names with their
	// volume names (as named in podSpec).
	// The unused PV will have the slice value set to nil.
	VolumeToPodsMap map[*v1.PersistentVolume][]PodVolume
	// Map of namespace qualified pod name wrt to the volumes they mount.
	// This map will not feature volumes which are not mounted by any pods.
	PodToVolumesMap map[string][]MountedVolume
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

// Volume metrics reported for a given pod
type PodVolumeMetrics struct {
	Volume   *v1.PersistentVolume
	Capacity float64
	Used     float64
	PodVolume
}

func NewKubeCluster(clusterName string, nodes []*v1.Node) *KubeCluster {
	kubeCluster := &KubeCluster{
		Name:             clusterName,
		Nodes:            make(map[string]*KubeNode),
		Namespaces:       make(map[string]*KubeNamespace),
		ClusterResources: make(map[metrics.ResourceType]*KubeDiscoveredResource),
	}
	kubeCluster.addNodes(nodes)
	if glog.V(3) {
		kubeCluster.logClusterNodes()
	}
	kubeCluster.computeClusterResources()
	return kubeCluster
}

func (kc *KubeCluster) addNodes(nodes []*v1.Node) *KubeCluster {
	for _, node := range nodes {
		// Create kubeNode with Allocatable resource as the
		// resource capacity
		kc.Nodes[node.Name] = NewKubeNode(node, kc.Name)
	}
	return kc
}

func (kc *KubeCluster) logClusterNodes() {
	for _, nodeEntity := range kc.Nodes {
		glog.Infof("Created node entity : %s", nodeEntity.String())
	}
}

// Sum the compute resource capacities from all the nodes to create the cluster resource capacities
func (kc *KubeCluster) computeClusterResources() {
	// sum the capacities of the node resources
	computeResources := make(map[metrics.ResourceType]float64)
	for _, node := range kc.Nodes {
		nodeActive := util.NodeIsReady(node.Node) && util.NodeIsSchedulable(node.Node)
		if nodeActive {
			// Iterate over all ready and schedulable compute resource types
			for _, rtList := range metrics.KubeComputeResourceTypes {
				for _, rt := range rtList {
					// get the compute resource if it exists
					nodeResource, exists := node.ComputeResources[rt]
					if !exists {
						glog.Errorf("Missing %s resource in node %s", rt, node.Name)
						continue
					}
					// add the capacity to the cluster compute resource map
					computeCap, exists := computeResources[rt]
					if !exists {
						computeCap = nodeResource.Capacity
					} else {
						computeCap = computeCap + nodeResource.Capacity
					}
					computeResources[rt] = computeCap
				}
			}
		}
	}
	// create KubeDiscoveredResource object for each compute resource type
	for _, rtList := range metrics.KubeComputeResourceTypes {
		for _, rt := range rtList {
			capacity := computeResources[rt]
			r := &KubeDiscoveredResource{
				Type:     rt,
				Capacity: capacity,
			}
			kc.ClusterResources[rt] = r
		}
	}
}

func (kc *KubeCluster) GetKubeNamespace(namespace string) *KubeNamespace {
	kubeNamespace, exists := kc.Namespaces[namespace]
	if !exists {
		glog.Errorf("Cannot find KubeNamespace entity for namespace %s", namespace)
		return nil
	}
	return kubeNamespace
}

// Summary object to get the nodes, quotas and namespaces in the cluster
type ClusterSummary struct {
	*KubeCluster
	// Computed
	NodeMap                  map[string]*v1.Node
	NodeList                 []*v1.Node
	NamespaceMap             map[string]*KubeNamespace
	NodeNameUIDMap           map[string]string
	NamespaceUIDMap          map[string]string
	PodClusterIDToServiceMap map[string]*v1.Service
	NodeNameToPodMap         map[string][]*v1.Pod
}

func CreateClusterSummary(kubeCluster *KubeCluster) *ClusterSummary {
	clusterSummary := &ClusterSummary{
		KubeCluster:              kubeCluster,
		NodeMap:                  make(map[string]*v1.Node),
		NodeList:                 []*v1.Node{},
		NamespaceMap:             make(map[string]*KubeNamespace),
		NodeNameUIDMap:           make(map[string]string),
		NamespaceUIDMap:          make(map[string]string),
		PodClusterIDToServiceMap: make(map[string]*v1.Service),
		NodeNameToPodMap:         make(map[string][]*v1.Pod),
	}

	clusterSummary.computeNodeMap()
	clusterSummary.computeQuotaMap()
	clusterSummary.computePodToServiceMap()

	return clusterSummary
}

func (summary *ClusterSummary) SetRunningPodsOnNode(node *v1.Node, pods []*v1.Pod) {

	if node == nil {
		glog.Errorf("Null node while setting pods for node")
		return
	}

	glog.Infof("Found %d pods for node %s", len(pods), node.Name)
	summary.NodeNameToPodMap[node.Name] = pods
}

func (summary *ClusterSummary) computeNodeMap() {
	summary.NodeMap = make(map[string]*v1.Node)
	summary.NodeNameUIDMap = make(map[string]string)
	for nodeName, node := range summary.Nodes {
		summary.NodeMap[nodeName] = node.Node
		summary.NodeNameUIDMap[nodeName] = string(node.Node.UID)
		summary.NodeList = append(summary.NodeList, node.Node)
	}
	return
}

func (getter *ClusterSummary) computeQuotaMap() {
	getter.NamespaceMap = make(map[string]*KubeNamespace)
	getter.NamespaceUIDMap = make(map[string]string)
	for namespaceName, namespace := range getter.Namespaces {
		getter.NamespaceMap[namespaceName] = namespace
		getter.NamespaceUIDMap[namespace.Name] = namespace.UID
	}
	return
}

func (summary *ClusterSummary) computePodToServiceMap() {
	summary.PodClusterIDToServiceMap = make(map[string]*v1.Service)

	for svc, podList := range summary.Services {
		for _, podClusterID := range podList {
			summary.PodClusterIDToServiceMap[podClusterID] = svc
		}
	}
}

// =================================================================================================
const (
	DEFAULT_METRIC_VALUE          float64 = 0.0
	DEFAULT_METRIC_CAPACITY_VALUE         = 1e12
)

// The node in the cluster
type KubeNode struct {
	*KubeEntity
	*v1.Node

	// node properties
	NodeCpuFrequency float64 // Set by the metrics collector which processes this node
}

// Create a KubeNode entity with compute resources to represent a node in the cluster
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

// Parse the Node instances returned by the kubernetes API to get the IP address
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
		cpuCapacityCore := util.MetricMilliToUnit(float64(ctnCpuCapacityMilliCore))
		return cpuCapacityCore
	}

	if computeResourceType == metrics.Memory ||
		computeResourceType == metrics.MemoryRequest {
		ctnMemoryCapacityBytes := resourceList.Memory().Value()
		memoryCapacityKiloBytes := util.Base2BytesToKilobytes(float64(ctnMemoryCapacityBytes))
		return memoryCapacityKiloBytes
	}
	return DEFAULT_METRIC_VALUE
}

// K8s controller in the cluster
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

// Construct controller full name by: "namespace/controllerType/controllerName"
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

// =================================================================================================
// The namespace in the cluster
type KubeNamespace struct {
	*KubeEntity
	// List of quotas defined for a namespace
	QuotaList               []*v1.ResourceQuota
	AverageNodeCpuFrequency float64
	QuotaDefined            map[metrics.ResourceType]bool
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

// Create default KubeNamespace object.
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
	return kubeNamespace
}

// Combining the quota limits for various resource quota objects defined in the namespace.
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

		for resource, _ := range resourceHardList {
			// This will return resourceType as "limits.cpu" for resource as both "cpu" and "limits.cpu"
			// Likewise for memory.
			resourceType, isAllocationType := metrics.KubeQuotaResourceTypes[resource]
			if !isAllocationType { // skip if it is not a allocation type resource
				continue
			}
			// Quota is defined for the quota resource
			kubeNamespace.QuotaDefined[resourceType] = true

			// Parse the CPU  values into number of cores, Memory values into KBytes
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
// CPU is represented in number of cores, Memory in KBytes
func parseAllocationResourceValue(resource v1.ResourceName, allocationResourceType metrics.ResourceType, resourceList v1.ResourceList) float64 {

	if allocationResourceType == metrics.CPULimitQuota || allocationResourceType == metrics.CPURequestQuota {
		quantity := resourceList[resource]
		cpuMilliCore := quantity.MilliValue()
		cpuCore := util.MetricMilliToUnit(float64(cpuMilliCore))
		return cpuCore
	}

	if allocationResourceType == metrics.MemoryLimitQuota || allocationResourceType == metrics.MemoryRequestQuota {
		quantity := resourceList[resource]
		memoryBytes := quantity.Value()
		memoryKiloBytes := util.Base2BytesToKilobytes(float64(memoryBytes))
		return memoryKiloBytes
	}
	return DEFAULT_METRIC_VALUE
}

// =================================================================================================
// The volumes in the cluster
type KubeVolume struct {
	*KubeEntity
	*v1.PersistentVolume
	ClusterName string
}

// Create a KubeVolume entity representing a volume in the cluster
func NewKubeVolume(pv *v1.PersistentVolume, clusterName string) *KubeVolume {
	entity := NewKubeEntity(metrics.VolumeType, clusterName,
		pv.ObjectMeta.Namespace, pv.ObjectMeta.Name,
		string(pv.ObjectMeta.UID))

	volumeEntity := &KubeVolume{
		KubeEntity:       entity,
		PersistentVolume: pv,
	}

	return volumeEntity
}
