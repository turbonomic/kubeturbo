package repository

import (
	"k8s.io/client-go/pkg/api/v1"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

type KubeCluster struct {
	Name string
	Nodes map[string]*KubeNode
	Namespaces map[string]*KubeNamespace
	ClusterResources map[metrics.ResourceType]*KubeDiscoveredResource
	NodeNameUIDMap map[string]string
	QuotaNameUIDMap map[string]string
}

type kubeClusterGetter struct {
	NodeMap map[string]*v1.Node
	QuotaMap map[string]*KubeQuota
}

// TODO: create a cluster access service to get cluster items

func (cluster *KubeCluster) GetKubeNode(nodeName string) *KubeNode {
	return cluster.Nodes[nodeName]
}

func (cluster *KubeCluster) GetNodes() []*v1.Node {
	var nodeList []*v1.Node
	for _, nodeEntity := range cluster.Nodes {
		nodeList = append(nodeList, nodeEntity.Node)
	}
	return nodeList
}

func (cluster *KubeCluster) GetQuotas() map[string]*KubeQuota {
	quotaMap := make(map[string]*KubeQuota)
	for _, namespace := range cluster.Namespaces {
		if namespace.Quota == nil {
			continue
		}
		quotaMap[namespace.Quota.Name] = namespace.Quota
	}
	return quotaMap
}

func (cluster *KubeCluster) GetQuota(namespace string) *KubeQuota {
	kubeNamespace, exists := cluster.Namespaces[namespace]
	if !exists {
		return nil
	}
	return kubeNamespace.Quota
}

type KubeNamespace struct {
	Name string
	Quota *KubeQuota
}

type KubeEntity struct {
	localId          string
	ClusterName      string
	Namespace        string
	Name             string
	UID              string
	ComputeResources map[metrics.ResourceType]*KubeDiscoveredResource // resources from the environment
	AllocationResources map[metrics.ResourceType]*KubeDiscoveredResource // resources from the environment
}

func NewKubeEntity() *KubeEntity {
	return &KubeEntity{
		ComputeResources: make(map[metrics.ResourceType]*KubeDiscoveredResource),
		AllocationResources: make(map[metrics.ResourceType]*KubeDiscoveredResource),
	}
}
type KubeNode struct {
	*KubeEntity
	Node *v1.Node
}

func (nodeEntity *KubeNode) GetCPUCapacity() float64 {
	value, _ := GetResource(metrics.CPULimit, nodeEntity.ComputeResources)
	return value.Capacity
}

func (nodeEntity *KubeNode) GetMemoryCapacity() float64 {
	value, _ := GetResource(metrics.MemoryLimit, nodeEntity.ComputeResources)
	return value.Capacity
}

type KubeQuota struct {
	*KubeEntity
	QuotaList      []*v1.ResourceQuota
	// map of resource bought from each node in the cluster
	ResourceBought map[*KubeNode]map[metrics.ResourceType]*KubeDiscoveredResource
	NodeProviders map[*KubeNode]*KubeResourceProvider
}

func (quota *KubeQuota) GetCPUCapacity() float64 {
	resource, err := GetResource(metrics.CPULimit, quota.ComputeResources)
	if err == nil {
		return resource.Capacity
	}
	return 0
}

func (quota *KubeQuota) GetMemoryCapacity() float64 {
	resource, err := GetResource(metrics.MemoryLimit, quota.ComputeResources)
	if err == nil {
		return resource.Capacity
	}
	return 0
}

func (quota *KubeQuota) SetNodeResources(node *KubeNode, bought map[metrics.ResourceType]*KubeDiscoveredResource) {
	quota.ResourceBought[node] = bought
}

// =================================================================================================

type KubeDiscoveredResource struct {
	Type         metrics.ResourceType
	Capacity     float64
	Used         float64
}

type KubeResourceProvider struct {
	Name string
	BoughtResources map[metrics.ResourceType]*KubeBoughtResource
}

type KubeBoughtResource struct {
	Type         metrics.ResourceType
	Used         float64
	Reservation float64
}

// =================================================================================================
type PodMetrics struct {
	Pod 		*v1.Pod
	Node		*v1.Node
	PodName 	string
	PodId 		string
	QuotaName 	string
	NodeName 	string
	PodKey  string
	ComputeUsed 	map[metrics.ResourceType]float64
	ComputeCap 	map[metrics.ResourceType]float64
	AllocationCap 	map[metrics.ResourceType]float64
	AllocationUsed 	map[metrics.ResourceType]float64
}

type NodeMetrics struct {
	NodeName string
	NodeKey  string
	ComputeUsed 	map[metrics.ResourceType]float64
	ComputeCap 	map[metrics.ResourceType]float64
	AllocationUsed 	map[metrics.ResourceType]float64
	AllocationCap 	map[metrics.ResourceType]float64
}

type QuotaMetrics struct {
	QuotaName           string
	AllocationBoughtMap map[string]map[metrics.ResourceType]float64
	AllocationCap       map[metrics.ResourceType]float64
	AllocationUsed       map[metrics.ResourceType]float64
}

func (quotaMetric *QuotaMetrics) CreateNodeMetrics(nodeName string, expectedQuotaResources []metrics.ResourceType) {
	emptyMap := make(map[metrics.ResourceType]float64)
	for _, rt := range expectedQuotaResources {
		emptyMap[rt] = 0.0
	}
	if quotaMetric.AllocationBoughtMap == nil {
		quotaMetric.AllocationBoughtMap = make(map[string]map[metrics.ResourceType]float64)
	}
	quotaMetric.AllocationBoughtMap[nodeName] = emptyMap
}

func GetResource(resourceType metrics.ResourceType, resourceMap map[metrics.ResourceType]*KubeDiscoveredResource) (*KubeDiscoveredResource, error) {
	resource, exists := resourceMap[resourceType]
	if !exists {
		return nil, fmt.Errorf("%s missing\n", resourceType)
	}
	return resource, nil
}

func CreateResources(resourceTypes []metrics.ResourceType) []*KubeDiscoveredResource {
	resources := []*KubeDiscoveredResource{}
	for _, resourceType := range resourceTypes {
		resource := &KubeDiscoveredResource {
			Type: resourceType,
			Used: 0.0,
		}
		resources = append(resources, resource)
	}
	return resources
}

// =================================================================================================

func CreateDefaultQuota(namespace string, clusterResources map[metrics.ResourceType]*KubeDiscoveredResource) *KubeQuota {
	quota := &KubeQuota{
		KubeEntity: NewKubeEntity(),
	}
	quota.Name = namespace
	quota.Namespace = namespace

	finalResourceMap := make(map[metrics.ResourceType]*KubeDiscoveredResource)
	for _, rt := range metrics.ComputeAllocationResources {
		r := &KubeDiscoveredResource{
			Type: rt,
		}
		finalResourceMap[r.Type] = r
		computeType, exists := metrics.AllocationToComputeMap[rt]
		if exists {
			computeResource, hasCompute := clusterResources[computeType]
			if hasCompute {
				r.Capacity = computeResource.Capacity
			}
		}
	}
	quota.AllocationResources = finalResourceMap

	fmt.Printf("****** DEFAULT Quota ===> %s\n", quota.Name)
	for _, resource := range quota.AllocationResources {
		fmt.Printf("\t ****** resource %s: cap=%f used=%f\n", resource.Type, resource.Capacity, resource.Used)
	}
	return quota
}
