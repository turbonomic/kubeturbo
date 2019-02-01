package repository

import (
	"bytes"
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"k8s.io/api/core/v1"
	"strings"
)

// Kube Cluster represents the Kubernetes cluster. This object is immutable between discoveries.
// New discovery will bring in changes to the nodes and namespaces
// Aggregate structure for nodes, namespaces and quotas
type KubeCluster struct {
	Name             string
	Nodes            map[string]*KubeNode
	Namespaces       map[string]*KubeNamespace
	ComputeResources map[metrics.ResourceType]float64
}

func NewKubeCluster(clusterName string) *KubeCluster {
	kubeCluster := &KubeCluster{
		Name:       clusterName,
		Nodes:      make(map[string]*KubeNode),
		Namespaces: make(map[string]*KubeNamespace),
	}
	return kubeCluster
}

func (kubeCluster *KubeCluster) LogClusterNodes() {
	for _, nodeEntity := range kubeCluster.Nodes {
		glog.Infof("Created node entity : %s\n", nodeEntity.String())
	}
}

func (kubeCluster *KubeCluster) LogClusterNamespaces() {
	for _, namespace := range kubeCluster.Namespaces {
		glog.Infof("Created namespace entity : %s\n", namespace.String())
	}
}

func (kubeCluster *KubeCluster) SetNodeEntity(nodeEntity *KubeNode) {
	if kubeCluster.Nodes == nil {
		kubeCluster.Nodes = make(map[string]*KubeNode)
	}
	kubeCluster.Nodes[nodeEntity.Name] = nodeEntity
}

func (kubeCluster *KubeCluster) GetNodeEntity(nodeName string) (*KubeNode, error) {
	if kubeCluster.Nodes == nil {
		return nil, fmt.Errorf("Null node %s", nodeName)
	}
	kubeNode, exists := kubeCluster.Nodes[nodeName]
	if !exists {
		return nil, fmt.Errorf("Null node %s", nodeName)
	}
	return kubeNode, nil
}

// Sum the compute resource capacities from all the nodes to create the cluster resource capacities
func (kubeCluster *KubeCluster) computeClusterResources() {
	if kubeCluster.Nodes == nil {
		glog.Errorf("No nodes in the cluster")
		return
	}
	// sum the capacities of the node resources
	computeResources := make(map[metrics.ResourceType]float64)
	for _, node := range kubeCluster.Nodes {
		// Iterate over all compute resource types
		for _, rt := range metrics.KubeComputeResourceTypes {
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

	kubeCluster.ComputeResources = computeResources
}

func (cluster *KubeCluster) GetKubeNode(nodeName string) *KubeNode {
	return cluster.Nodes[nodeName]
}

func (cluster *KubeCluster) GetQuota(namespace string) *KubeQuota {
	kubeNamespace, exists := cluster.Namespaces[namespace]
	if !exists {
		return nil
	}
	return kubeNamespace.Quota
}

// Summary object to get the nodes, quotas and namespaces in the cluster
type ClusterSummary struct {
	*KubeCluster
	// Computed
	NodeMap         map[string]*v1.Node
	NodeList        []*v1.Node
	QuotaMap        map[string]*KubeQuota
	NodeNameUIDMap  map[string]string
	QuotaNameUIDMap map[string]string
}

func CreateClusterSummary(kubeCluster *KubeCluster) *ClusterSummary {
	clusterSummary := &ClusterSummary{
		KubeCluster:     kubeCluster,
		NodeMap:         make(map[string]*v1.Node),
		NodeList:        []*v1.Node{},
		QuotaMap:        make(map[string]*KubeQuota),
		NodeNameUIDMap:  make(map[string]string),
		QuotaNameUIDMap: make(map[string]string),
	}

	clusterSummary.computeNodeMap()
	clusterSummary.computeQuotaMap()

	return clusterSummary
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
	nodeEntity.UpdateResources(apiNode)
	return nodeEntity
}

// Set the compute resources and IP property in the node entity
func (nodeEntity *KubeNode) UpdateResources(apiNode *v1.Node) {
	// Node compute resources
	resourceAllocatableList := apiNode.Status.Allocatable
	for resource, _ := range resourceAllocatableList {
		computeResourceType, isComputeType := metrics.KubeComputeResourceTypes[resource]
		if !isComputeType {
			continue
		}
		capacityValue := parseResourceValue(computeResourceType, resourceAllocatableList)
		nodeEntity.AddComputeResource(computeResourceType, float64(capacityValue), DEFAULT_METRIC_VALUE)
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
	if computeResourceType == metrics.CPU {
		ctnCpuCapacityMilliCore := resourceList.Cpu().MilliValue()
		cpuCapacityCore := float64(ctnCpuCapacityMilliCore) / util.MilliToUnit
		return cpuCapacityCore
	}

	if computeResourceType == metrics.Memory {
		ctnMemoryCapacityBytes := resourceList.Memory().Value()
		memoryCapacityKiloBytes := float64(ctnMemoryCapacityBytes) / util.KilobytesToBytes
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
	for _, rt := range metrics.ComputeAllocationResources {
		computeType, exists := metrics.AllocationToComputeMap[rt] //corresponding compute resource
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
			glog.Errorf("%s : cannot find cluster compute resource type for allocation %s\n",
				quota.Name, rt)
		}
	}

	// node providers will be added by the quota discovery worker when the usage values are available
	glog.V(3).Infof("Created default quota for namespace : %s\n", namespace)
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
			resourceType, isAllocationType := metrics.KubeAllocatonResourceTypes[resource]
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
	glog.V(2).Infof("Reconciled Quota %s from ===> %s "+
		"[CPULimit:%t, MemoryLimit:%t CPURequest:%t, MemoryRequest:%t]",
		quotaEntity.Name, quotaListStr,
		quotaEntity.AllocationDefined[metrics.CPULimit],
		quotaEntity.AllocationDefined[metrics.MemoryLimit],
		quotaEntity.AllocationDefined[metrics.CPURequest],
		quotaEntity.AllocationDefined[metrics.MemoryRequest])
}

// Parse the CPU and Memory resource values.
// CPU is represented in number of cores, Memory in KBytes
func parseAllocationResourceValue(resource v1.ResourceName, allocationResourceType metrics.ResourceType, resourceList v1.ResourceList) float64 {

	if allocationResourceType == metrics.CPULimit || allocationResourceType == metrics.CPURequest {
		quantity := resourceList[resource]
		cpuMilliCore := quantity.MilliValue()
		cpuCore := float64(cpuMilliCore) / util.MilliToUnit
		return cpuCore
	}

	if allocationResourceType == metrics.MemoryLimit || allocationResourceType == metrics.MemoryRequest {
		quantity := resourceList[resource]
		memoryBytes := quantity.Value()
		memoryKiloBytes := float64(memoryBytes) / util.KilobytesToBytes
		return memoryKiloBytes
	}
	return DEFAULT_METRIC_VALUE
}

func (quotaEntity *KubeQuota) AddNodeProvider(nodeUID string,
	allocationBought map[metrics.ResourceType]float64) {
	if allocationBought == nil {
		glog.V(2).Infof("%s : missing metrics for node %s\n", quotaEntity.Name, nodeUID)
		allocationBought := make(map[metrics.ResourceType]float64)
		for _, rt := range metrics.ComputeAllocationResources {
			allocationBought[rt] = DEFAULT_METRIC_VALUE
		}
	}

	for resourceType, resourceUsed := range allocationBought {
		quotaEntity.AddProviderResource(metrics.NodeType, nodeUID, resourceType, resourceUsed)
	}
}
