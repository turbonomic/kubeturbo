package repository

import (
	"bytes"
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"k8s.io/client-go/pkg/api/v1"
	"strings"
)

// Kube Cluster represents the Kubernetes cluster. This object is immutable between discoveries.
// New discovery will bring in changes to the nodes and namespaces
// Aggregate structure for nodes, namespaces and quotas
type KubeCluster struct {
	Name       string
	Nodes      map[string]*KubeNode
	Namespaces map[string]*KubeNamespace
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

// =================================================================================================
const DEFAULT_METRIC_VALUE float64 = 0.0

// The node in the cluster
type KubeNode struct {
	*KubeEntity
	*v1.Node

	// node properties
	NodeCpuFrequency float64 // Set by the metrics collector which processes this node
	SystemUUID       string
	IPAddress        string
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
	// node compute resources
	resourceAllocatableList := apiNode.Status.Allocatable
	for resource, _ := range resourceAllocatableList {
		computeResourceType, isComputeType := metrics.KubeComputeResourceTypes[resource]
		if !isComputeType {
			continue
		}
		capacityValue := parseResourceValue(computeResourceType, resourceAllocatableList)
		nodeEntity.AddComputeResource(computeResourceType, float64(capacityValue), DEFAULT_METRIC_VALUE)
	}

	nodeSystemUUID := apiNode.Status.NodeInfo.SystemUUID
	if nodeSystemUUID == "" {
		glog.Errorf("Invalid SystemUUID for node %s", apiNode.Name)
	} else {
		nodeEntity.SystemUUID = strings.ToLower(nodeSystemUUID)
	}

	var nodeStitchingIP string
	nodeAddresses := apiNode.Status.Addresses
	// Use external IP if it is available. Otherwise use legacy host IP.
	for _, nodeAddress := range nodeAddresses {
		if nodeAddress.Type == v1.NodeExternalIP && nodeAddress.Address != "" {
			nodeStitchingIP = nodeAddress.Address
		}
		if nodeStitchingIP == "" && nodeAddress.Address != "" && nodeAddress.Type == v1.NodeInternalIP {
			nodeStitchingIP = nodeAddress.Address
		}
	}

	if nodeStitchingIP == "" {
		glog.Errorf("Failed to find stitching IP for node %s: it does not have either external IP or legacy "+
			"host IP.", apiNode.Name)
	} else {
		nodeEntity.IPAddress = nodeStitchingIP
	}
	glog.V(2).Infof("Created node entity : %s\n", nodeEntity.String())
	return nodeEntity
}

func (nodeEntity *KubeNode) String() string {
	var buffer bytes.Buffer
	buffer.WriteString(nodeEntity.KubeEntity.String())
	line := fmt.Sprintf("SystemUUID:%s\n", nodeEntity.SystemUUID)
	buffer.WriteString(line)
	line = fmt.Sprintf("IPAddress:%s\n", nodeEntity.IPAddress)
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

// The quota defined for a namespace
type KubeQuota struct {
	*KubeEntity
	QuotaList               []*v1.ResourceQuota
	AverageNodeCpuFrequency float64
}

// Create a Quota object for a namespace.
// The resource quota limits are based on the cluster compute resource limits.
func CreateDefaultQuota(clusterName, namespace string,
	clusterResources map[metrics.ResourceType]*KubeDiscoveredResource) *KubeQuota {
	quota := &KubeQuota{
		KubeEntity: NewKubeEntity(metrics.QuotaType, clusterName,
			namespace, namespace, namespace),
		QuotaList: []*v1.ResourceQuota{},
	}

	// create quota allocation resources
	for _, rt := range metrics.ComputeAllocationResources {
		computeType, exists := metrics.AllocationToComputeMap[rt] //corresponding compute resource
		if exists {
			var capacity float64
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
	glog.V(2).Infof("Created default quota for namespace : %s\n", namespace)
	return quota
}

// Combining the quota limits for various resource quota objects defined in the namesapce.
// Multiple quota limits for the same resource, if any, are reconciled by selecting the
// most restrictive limit value.
func (quotaEntity *KubeQuota) ReconcileQuotas(quotas []*v1.ResourceQuota) {
	quotaListStr := ""
	// Quota resources by collecting resources from the list of resource quota objects
	for _, item := range quotas {
		quotaListStr = quotaListStr + item.Name + ","
		// Resources in each quota
		resourceStatus := item.Status
		resourceHardList := resourceStatus.Hard
		resourceUsedList := resourceStatus.Used

		for resource, _ := range resourceHardList {
			resourceType, isAllocationType := metrics.KubeAllocatonResourceTypes[resource]
			if !isAllocationType { // skip if it is not a allocation type resource
				continue
			}
			capacityValue := parseAllocationResourceValue(resource, resourceType, resourceHardList)
			usedValue := parseAllocationResourceValue(resource, resourceType, resourceUsedList)

			existingResource, err := quotaEntity.GetAllocationResource(resourceType)
			if err == nil {
				// update the existing resource with the one in this quota
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

	glog.V(2).Infof("Reconciled Quota %s from ===> %s \n", quotaEntity.Name, quotaListStr)
}

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
