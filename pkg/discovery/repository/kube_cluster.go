package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"k8s.io/client-go/pkg/api/v1"
)

// Kube Cluster represents the Kubernetes cluster. This object is immutable between discoveries.
// New discovery will bring in changes to the nodes and namespaces
// Aggregate structure for nodes, namespaces and quotas
type KubeCluster struct {
	Name             string
	Nodes            map[string]*KubeNode
	Namespaces       map[string]*KubeNamespace
	ClusterResources map[metrics.ResourceType]*KubeDiscoveredResource
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
	if summary.Nodes == nil {
		return
	}
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
	if getter.Namespaces == nil {
		return
	}
	getter.QuotaMap = make(map[string]*KubeQuota)
	getter.QuotaNameUIDMap = make(map[string]string)
	for namespaceName, namespace := range getter.Namespaces {
		getter.QuotaMap[namespaceName] = namespace.Quota
		getter.QuotaNameUIDMap[namespace.Name] = namespace.Quota.UID
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

func (quotaEntity *KubeQuota) AddNodeProvider(nodeUID string,
						allocationBought map[metrics.ResourceType]float64) {
	if allocationBought == nil {
		return
	}
	_, exists := quotaEntity.ProviderMap[nodeUID]
	if !exists {
		provider := &KubeResourceProvider{
			EntityType:       metrics.NodeType,
			UID:              nodeUID,
			BoughtCompute:    make(map[metrics.ResourceType]*KubeBoughtResource),
			BoughtAllocation: make(map[metrics.ResourceType]*KubeBoughtResource),
		}
		quotaEntity.ProviderMap[nodeUID] = provider
	}

	provider, _ := quotaEntity.ProviderMap[nodeUID]
	bought := make(map[metrics.ResourceType]*KubeBoughtResource)
	for resourceType, resourceUsed := range allocationBought {
		kubeResource := &KubeBoughtResource{
			Type: resourceType,
			Used: resourceUsed,
		}
		bought[resourceType] = kubeResource
	}
	provider.BoughtAllocation = bought
}


