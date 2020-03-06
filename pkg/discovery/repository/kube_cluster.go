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

func (kc *KubeCluster) GetQuota(namespace string) *KubeQuota {
	kubeNamespace, exists := kc.Namespaces[namespace]
	if !exists {
		return nil
	}
	return kubeNamespace.Quota
}

// Summary object to get the nodes, quotas and namespaces in the cluster
type ClusterSummary struct {
	*KubeCluster
	// Computed
	NodeMap                  map[string]*v1.Node
	NodeList                 []*v1.Node
	QuotaMap                 map[string]*KubeQuota
	NodeNameUIDMap           map[string]string
	QuotaNameUIDMap          map[string]string
	PodClusterIDToServiceMap map[string]*v1.Service
	NodeNameToPodMap         map[string][]*v1.Pod
}

func CreateClusterSummary(kubeCluster *KubeCluster) *ClusterSummary {
	clusterSummary := &ClusterSummary{
		KubeCluster:              kubeCluster,
		NodeMap:                  make(map[string]*v1.Node),
		NodeList:                 []*v1.Node{},
		QuotaMap:                 make(map[string]*KubeQuota),
		NodeNameUIDMap:           make(map[string]string),
		QuotaNameUIDMap:          make(map[string]string),
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
	getter.QuotaMap = make(map[string]*KubeQuota)
	getter.QuotaNameUIDMap = make(map[string]string)
	for namespaceName, namespace := range getter.Namespaces {
		if namespace.Quota != nil {
			getter.QuotaMap[namespaceName] = namespace.Quota
			getter.QuotaNameUIDMap[namespace.Name] = namespace.Quota.UID
		}
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
const DEFAULT_METRIC_VALUE float64 = 0.0

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

// =================================================================================================
// The namespace in the cluster
type KubeNamespace struct {
	Name        string
	ClusterName string
	Quota       *KubeQuota
}

func (namespaceEntity *KubeNamespace) String() string {
	var buffer bytes.Buffer
	line := fmt.Sprintf("Namespace:%s\n", namespaceEntity.Name)
	buffer.WriteString(line)
	line = fmt.Sprintf(namespaceEntity.Quota.String())
	buffer.WriteString(line)
	return buffer.String()
}

// The quota defined for a namespace
type KubeQuota struct {
	*KubeEntity
	QuotaList               []*v1.ResourceQuota
	AverageNodeCpuFrequency float64
	AllocationDefined       map[metrics.ResourceType]bool
}

func (quotaEntity *KubeQuota) String() string {
	var buffer bytes.Buffer

	if len(quotaEntity.QuotaList) > 0 {
		var quotaNames []string
		for _, quota := range quotaEntity.QuotaList {
			quotaNames = append(quotaNames, quota.Name)
		}
		quotaStr := strings.Join(quotaNames, ",")
		line := fmt.Sprintf("Contains resource quota(s) : %s\n", quotaStr)
		buffer.WriteString(line)
	}

	var prefix string
	if len(quotaEntity.QuotaList) == 0 {
		prefix = "Default "
	} else {
		prefix = "Reconciled "
	}
	buffer.WriteString(prefix)
	buffer.WriteString(quotaEntity.KubeEntity.String())

	return buffer.String()
}

// Create a Quota object for a namespace.
// The resource quota limits are based on the cluster compute resource limits.
func CreateDefaultQuota(clusterName, namespace, uuid string,
	clusterResources map[metrics.ResourceType]*KubeDiscoveredResource) *KubeQuota {
	quota := &KubeQuota{
		KubeEntity: NewKubeEntity(metrics.QuotaType, clusterName,
			namespace, namespace, uuid),
		QuotaList:         []*v1.ResourceQuota{},
		AllocationDefined: make(map[metrics.ResourceType]bool),
	}

	// create quota allocation resources
	for _, rt := range metrics.QuotaResources {
		computeType, exists := metrics.QuotaToComputeMap[rt] //corresponding compute resource
		if exists {
			capacity := float64(0.0)
			computeResource, hasCompute := clusterResources[computeType]
			if hasCompute {
				capacity = computeResource.Capacity
			}
			quota.AddAllocationResource(rt, capacity, DEFAULT_METRIC_VALUE)
			// used values for the sold resources are obtained while parsing the quota objects
			// or by adding the usages of pod compute resources running in the namespace
		} else {
			glog.Errorf("Cannot find allocation resource type %s for namespace %s",
				rt, quota.Name)
		}
	}

	// node providers will be added by the quota discovery worker when the usage values are available
	glog.V(3).Infof("Created default quota for namespace : %s", namespace)
	return quota
}

// Combining the quota limits for various resource quota objects defined in the namesapce.
// Multiple quota limits for the same resource, if any, are reconciled by selecting the
// most restrictive limit value.
func (quotaEntity *KubeQuota) ReconcileQuotas(quotas []*v1.ResourceQuota) {
	// Quota resources by collecting resources from the list of resource quota objects
	var quotaNames []string
	for _, item := range quotas {
		//quotaListStr = quotaListStr + item.Name + ","
		quotaNames = append(quotaNames, item.Name)
		// Resources in each quota
		resourceStatus := item.Status
		resourceHardList := resourceStatus.Hard
		resourceUsedList := resourceStatus.Used

		for resource, _ := range resourceHardList {
			resourceType, isAllocationType := metrics.KubeQuotaResourceTypes[resource]
			if !isAllocationType { // skip if it is not a allocation type resource
				continue
			}
			// Quota is defined for the allocation resource
			quotaEntity.AllocationDefined[resourceType] = true

			// Parse the CPU  values into number of cores, Memory values into KBytes
			capacityValue := parseAllocationResourceValue(resource, resourceType, resourceHardList)
			usedValue := parseAllocationResourceValue(resource, resourceType, resourceUsedList)

			// Update the quota entity resource with the most restrictive quota value
			existingResource, err := quotaEntity.GetAllocationResource(resourceType)
			if err == nil {
				if existingResource.Capacity == DEFAULT_METRIC_VALUE || capacityValue < existingResource.Capacity {
					existingResource.Capacity = capacityValue
					existingResource.Used = usedValue
				}
			} else {
				// create resource if it does not exist
				quotaEntity.AddAllocationResource(resourceType, capacityValue, usedValue)
			}
		}
	}
	quotaListStr := strings.Join(quotaNames, ",")
	glog.V(4).Infof("Reconciled Quota %s from %s"+
		"[CPUQuota:%t, MemoryQuota:%t CPURequestQuota:%t, MemoryRequestQuota:%t]",
		quotaEntity.Name, quotaListStr,
		quotaEntity.AllocationDefined[metrics.CPUQuota],
		quotaEntity.AllocationDefined[metrics.MemoryQuota],
		quotaEntity.AllocationDefined[metrics.CPURequestQuota],
		quotaEntity.AllocationDefined[metrics.MemoryRequestQuota])
}

// Parse the CPU and Memory resource values.
// CPU is represented in number of cores, Memory in KBytes
func parseAllocationResourceValue(resource v1.ResourceName, allocationResourceType metrics.ResourceType, resourceList v1.ResourceList) float64 {

	if allocationResourceType == metrics.CPUQuota || allocationResourceType == metrics.CPURequestQuota {
		quantity := resourceList[resource]
		cpuMilliCore := quantity.MilliValue()
		cpuCore := util.MetricMilliToUnit(float64(cpuMilliCore))
		return cpuCore
	}

	if allocationResourceType == metrics.MemoryQuota || allocationResourceType == metrics.MemoryRequestQuota {
		quantity := resourceList[resource]
		memoryBytes := quantity.Value()
		memoryKiloBytes := util.Base2BytesToKilobytes(float64(memoryBytes))
		return memoryKiloBytes
	}
	return DEFAULT_METRIC_VALUE
}

func (quotaEntity *KubeQuota) AddNodeProvider(nodeUID string,
	allocationBought map[metrics.ResourceType]float64) {
	if allocationBought == nil {
		glog.V(2).Infof("%s : missing metrics for node %s\n", quotaEntity.Name, nodeUID)
		allocationBought := make(map[metrics.ResourceType]float64)
		for _, rt := range metrics.QuotaResources {
			allocationBought[rt] = DEFAULT_METRIC_VALUE
		}
	}

	for resourceType, resourceUsed := range allocationBought {
		quotaEntity.AddProviderResource(metrics.NodeType, nodeUID, resourceType, resourceUsed)
	}
}
