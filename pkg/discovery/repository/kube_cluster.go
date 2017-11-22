package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"k8s.io/client-go/pkg/api/v1"
	"github.com/golang/glog"
)

// Kube Cluster represents the Kubernetes cluster. This object is immutable between discoveries.
// New discovery will bring in changes to the nodes and namespaces
// Aggregate structure for nodes, namespaces and quotas
type KubeCluster struct {
	Name             string
	Nodes            map[string]*KubeNode
	Namespaces       map[string]*KubeNamespace
	//ClusterResources map[metrics.ResourceType]*KubeDiscoveredResource
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
// The node in the cluster
type KubeNode struct {
	*KubeEntity
	*v1.Node
}

func NewKubeNode(apiNode *v1.Node, clusterName string) *KubeNode {
	entity := NewKubeEntity(metrics.NodeType, clusterName,
		apiNode.ObjectMeta.Namespace,apiNode.ObjectMeta.Name,
		string(apiNode.ObjectMeta.UID))

	nodeEntity := &KubeNode{
		KubeEntity: entity,
		Node: apiNode,
	}
	// node compute resources
	resourceAllocatableList := apiNode.Status.Allocatable
	for resource, _:= range resourceAllocatableList {
		computeResourceType, isComputeType := metrics.KubeComputeResourceTypes[resource]
		if !isComputeType {
			continue
		}
		quantity := resourceAllocatableList[resource]
		capacityValue := quantity.MilliValue()
		nodeEntity.AddComputeResource(computeResourceType, float64(capacityValue), 0.0)
	}
	return nodeEntity
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
	QuotaList []*v1.ResourceQuota
}

func NewKubeQuota(clusterName, namespace string) *KubeQuota {
	return &KubeQuota{
		KubeEntity: NewKubeEntity(metrics.QuotaType, clusterName,
						namespace, namespace, namespace),
		QuotaList:  []*v1.ResourceQuota{},
	}
}


// Return a single quota entity for the namespace by combining the quota limits for various
// resources from multiple quota objects defined in the namesapce.
// Multiple quota limits for the same resource, if any, are reconciled by selecting the
// most restrictive limit value.
func CreateKubeQuota(clusterName, namespace string, quotas []*v1.ResourceQuota) *KubeQuota {

	quota := &KubeQuota{
		KubeEntity: NewKubeEntity(metrics.QuotaType, clusterName,
			namespace, namespace, namespace),
		QuotaList:  []*v1.ResourceQuota{},
	}
	quotaListStr := ""
	// Quota resources by collecting resources from the list of resource quota objects
	// Quota resources by collecting resources from the list of resource quota objects
	for _, item := range quotas {
		quotaListStr = quotaListStr + item.Name +","
		// Resources in each quota
		resourceStatus := item.Status
		resourceHardList := resourceStatus.Hard
		resourceUsedList := resourceStatus.Used
		for resource, _:= range resourceHardList {
			resourceType, isAllocationType := metrics.KubeAllocatonResourceTypes[resource]
			if !isAllocationType {	// skip if it is not a allocation type resource
				continue
			}
			quantity := resourceHardList[resource]
			capacityValue := quantity.MilliValue()
			used := resourceUsedList[resource]
			usedValue := used.MilliValue()

			// create resource if it does not exist or update the capacity value
			_, err := quota.GetAllocationResource(resourceType)
			if err != nil  {
				quota.AddAllocationResource(resourceType,float64(capacityValue), float64(usedValue))
			}
			existingResource, _ := quota.GetAllocationResource(resourceType)
			// update the existing resource with the one in this quota
			if float64(capacityValue) < existingResource.Capacity  {
				existingResource.Capacity = float64(capacityValue)
				existingResource.Used = float64(usedValue)
			}
		}
	}

	glog.Infof("Reconciled Quota %s from ===> %s \n", quota.Name, quotaListStr)
	return quota
}

func (quotaEntity *KubeQuota) AddNodeProvider(nodeUID string,
						allocationBought map[metrics.ResourceType]float64) {
	if allocationBought == nil {
		return
	}
	for resourceType, resourceUsed := range allocationBought {
		quotaEntity.AddProviderResource(metrics.NodeType, nodeUID, resourceType, resourceUsed)
	}
}

