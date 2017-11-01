package processor

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// =================================================================================================
// Class to read the cluster data from the Kubernetes API server and create the KubeCluster entity
// to represent the cluster, the nodes and the namespaces
type ClusterProcessor struct {
	ClusterInfoScraper *cluster.ClusterScraper
}

// Query the Kubernetes API Server and Get the Namespace objects
func (processor *ClusterProcessor) ProcessCluster() (*repository.KubeCluster, error) {
	svcID, _ := processor.ClusterInfoScraper.GetKubernetesServiceID()

	kubeCluster := &repository.KubeCluster{
		Name: svcID,
		Nodes: make(map[string]*repository.KubeNode),
		Namespaces: make(map[string]*repository.KubeNamespace),
	}

	// Namespaces
	processor.processNamespaces(kubeCluster)
	// Nodes
	processor.processNodes(kubeCluster)

	kubeCluster.NodeNameUIDMap = make(map[string]string)
	//TODO:
	kubeCluster.QuotaNameUIDMap = make(map[string]string)
	//TODO:
	// Create cluster resources
	kubeCluster.ClusterResources = processor.computeClusterResources(kubeCluster.Nodes)

	for rt, cap := range kubeCluster.ClusterResources {
		fmt.Printf("^^^^^ default cluster resource %s = %f\n", rt, cap)
	}
	// Quotas
	quotaProcessor := &QuotaProcessor{clusterInfoScraper: processor.ClusterInfoScraper,}
	quotaEntityMap := quotaProcessor.ProcessResourceQuotas()
	// Attach quotas to namespaces
	for _, namespace := range kubeCluster.Namespaces {
		quotaEntity, exists := quotaEntityMap[namespace.Name]
		if !exists {
			fmt.Printf("Creating default quota for namespace %s\n", namespace.Name)
			quotaEntity = repository.CreateDefaultQuota(namespace.Name, kubeCluster.ClusterResources)
		}
		namespace.Quota = quotaEntity
	}
	return kubeCluster, nil
}

// Query the Kubernetes API Server and Get the Namespace objects
func (processor *ClusterProcessor) processNamespaces(cluster *repository.KubeCluster) (error) {
	namespaceList, err := processor.ClusterInfoScraper.GetNamespaces()
	if err != nil {
		return err
	}
	fmt.Printf("There are %d namespaces\n", len(namespaceList))

	//printNamespaceList(namespaceList.Items)
	namespaces := make(map[string]*repository.KubeNamespace)
	for _, item := range namespaceList{
		namespace := &repository.KubeNamespace{
			Name: item.Name,
		}
		namespaces[item.Name] = namespace
	}
	cluster.Namespaces = namespaces
	return nil
}

// Query the Kubernetes API Server and Get the Namespace objects
func (processor *ClusterProcessor) processNodes(cluster *repository.KubeCluster) (map[string]*repository.KubeNode, error)  {
	nodeList, err := processor.ClusterInfoScraper.GetAllNodes()
	if err != nil {
		return nil, err
	}
	fmt.Printf("There are %d nodes\n", len(nodeList))

	//printNamespaceList(namespaceList.Items)
	nodes := make(map[string]*repository.KubeNode)
	for _, item := range nodeList{
		entity:= &repository.KubeEntity{
			ClusterName:  "",
			Namespace: item.ObjectMeta.Namespace,
			Name: item.ObjectMeta.Name,
			UID: string(item.ObjectMeta.UID),
			ComputeResources: make(map[metrics.ResourceType]*repository.KubeDiscoveredResource),
		}
		nodeEntity := &repository.KubeNode{
			KubeEntity: entity,
			Node: item,
		}
		// node compute resources
		resourceAllocatableList := item.Status.Allocatable
		for resource, _:= range resourceAllocatableList {
			rt := metrics.KubeResourceTypes[resource]
			if !metrics.IsComputeType(rt) {
				continue
			}
			quantity := resourceAllocatableList[resource]
			capacityValue := quantity.MilliValue()
			r := &repository.KubeDiscoveredResource{
				Type: metrics.KubeResourceTypes[resource],
				Capacity: float64(capacityValue),
			}
			nodeEntity.ComputeResources[r.Type] = r
		}
		nodes[item.Name] = nodeEntity
	}
	cluster.Nodes = nodes
	return nodes, nil
}


func (processor *ClusterProcessor) computeClusterResources(nodes map[string]*repository.KubeNode) (map[metrics.ResourceType]*repository.KubeDiscoveredResource){
	computeResources := make(map[metrics.ResourceType]float64)
	for _, node := range nodes {
		for rt, resource := range node.ComputeResources {
			fmt.Printf("node %s resource %s = %f\n", node.Name, rt, resource.Capacity)
			if !metrics.IsComputeType(rt) {
				continue
			}
			computeCap, exists := computeResources[rt]
			if !exists {
				computeCap = resource.Capacity
			} else {
				computeCap = computeCap + resource.Capacity
			}
			computeResources[rt] = computeCap
		}
	}

	clusterResources := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
	for rt, capacity := range computeResources {
		r := &repository.KubeDiscoveredResource{
			Type: rt,
			Capacity: capacity,
		}
		clusterResources[rt] = r
	}
	return clusterResources
}

