package processor

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/golang/glog"
)

// Class to query the cluster data from the Kubernetes API server and create the KubeCluster entity
// to represent the cluster, the nodes and the namespaces.
type ClusterProcessor struct {
	ClusterInfoScraper *cluster.ClusterScraper
}

// Query the Kubernetes API Server and Get the Namespace objects
func (processor *ClusterProcessor) ProcessCluster() (*repository.KubeCluster, error) {
	svcID, err := processor.ClusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return nil, fmt.Errorf("Cannot obtain service ID for cluster %s\n", err)
	}

	kubeCluster := &repository.KubeCluster{
		Name: svcID,
		Nodes: make(map[string]*repository.KubeNode),
		Namespaces: make(map[string]*repository.KubeNamespace),
	}

	// Namespaces
	namespaces, err := processor.processNamespaces(kubeCluster.Name)
	if err != nil {
		return nil, fmt.Errorf("%s:%s\n", svcID, err)
	}
	kubeCluster.Namespaces = namespaces

	// Nodes
	nodes, err := processor.processNodes(kubeCluster.Name)
	if err != nil {
		return nil, fmt.Errorf("%s:%s\n", svcID, err)
	}
	kubeCluster.Nodes = nodes

	// Create cluster resources
	kubeCluster.ClusterResources = processor.computeClusterResources(kubeCluster.Nodes)

	for rt, cap := range kubeCluster.ClusterResources {
		glog.Infof("cluster resource %s has capacity = %f\n", rt, cap)
	}

	// Quotas
	quotaProcessor := &QuotaProcessor{
				clusterInfoScraper: processor.ClusterInfoScraper,
				clusterName: kubeCluster.Name,
			}
	quotaEntityMap := quotaProcessor.ProcessResourceQuotas()
	// Attach quotas to namespaces and create default quota object if one does not exist
	for _, namespace := range kubeCluster.Namespaces {
		quotaEntity, exists := quotaEntityMap[namespace.Name]
		if !exists {
			quotaEntity = processor.createDefaultQuota(kubeCluster.Name, namespace.Name,
									kubeCluster.ClusterResources)
		}
		namespace.Quota = quotaEntity
	}
	return kubeCluster, nil
}

// Query the Kubernetes API Server and Get the Namespace objects
func (processor *ClusterProcessor) processNamespaces(clusterName string) (map[string]*repository.KubeNamespace, error) {
	namespaceList, err := processor.ClusterInfoScraper.GetNamespaces()
	if err != nil {
		return nil, fmt.Errorf("Error getting namespaces for cluster %s:%s\n", clusterName, err)
	}
	glog.Infof("There are %d namespaces\n", len(namespaceList))

	namespaces := make(map[string]*repository.KubeNamespace)
	for _, item := range namespaceList{
		namespace := &repository.KubeNamespace{
			ClusterName: clusterName,
			Name: item.Name,
		}
		namespaces[item.Name] = namespace
	}
	return namespaces, nil
}

// Query the Kubernetes API Server and Get the Node objects
func (processor *ClusterProcessor) processNodes(clusterName string) (map[string]*repository.KubeNode, error)  {
	nodeList, err := processor.ClusterInfoScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("Error getting nodes for cluster %s:%s\n", clusterName, err)
	}
	glog.Infof("There are %d nodes\n", len(nodeList))

	nodes := make(map[string]*repository.KubeNode)
	for _, item := range nodeList{

		entity := repository.NewKubeEntity(metrics.NodeType, clusterName,
							item.ObjectMeta.Namespace,item.ObjectMeta.Name,
							string(item.ObjectMeta.UID))
		nodeEntity := &repository.KubeNode{
			KubeEntity: entity,
			Node: item,
		}
		// node compute resources
		resourceAllocatableList := item.Status.Allocatable
		for resource, _:= range resourceAllocatableList {
			computeResourceType, isComputeType := metrics.KubeComputeResourceTypes[resource]
			if !isComputeType {
				continue
			}
			quantity := resourceAllocatableList[resource]
			capacityValue := quantity.MilliValue()
			r := &repository.KubeDiscoveredResource{
				Type: computeResourceType,
				Capacity: float64(capacityValue),
			}
			nodeEntity.ComputeResources[r.Type] = r
		}
		glog.V(2).Infof("Discovered node entity : %s\n", nodeEntity.KubeEntity.String())
		nodes[item.Name] = nodeEntity
	}

	return nodes, nil
}

func (processor *ClusterProcessor) computeClusterResources(nodes map[string]*repository.KubeNode) (map[metrics.ResourceType]*repository.KubeDiscoveredResource){
	computeResources := make(map[metrics.ResourceType]float64)
	for _, node := range nodes {
		for rt, resource := range node.ComputeResources {
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

// =================================================================================================
// Create a Quota object for namespaces that do have resource quota objects defined.
// The resource quota limits are based on the cluster compute resource limits.
func (processor *ClusterProcessor) createDefaultQuota(clusterName, namespace string,
							clusterResources map[metrics.ResourceType]*repository.KubeDiscoveredResource,
							) *repository.KubeQuota {
	quota := repository.NewKubeQuota(clusterName, namespace)

	// create quota allocation resources based on the cluster compute resources
	finalResourceMap := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
	for _, rt := range metrics.ComputeAllocationResources {
		r := &repository.KubeDiscoveredResource{
			Type: rt,
		}
		finalResourceMap[r.Type] = r
		computeType, exists := metrics.AllocationToComputeMap[rt] //corresponding compute resource
		if exists {
			computeResource, hasCompute := clusterResources[computeType]
			if hasCompute {
				r.Capacity = computeResource.Capacity
			}
		}
	}
	quota.AllocationResources = finalResourceMap

	glog.V(2).Infof("Created default quota for namespace : %s\n", namespace)
	return quota
}
