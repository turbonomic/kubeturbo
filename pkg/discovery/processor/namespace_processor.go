package processor

import (
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// Class to query the multiple namespace objects data from the Kubernetes API server
// and create the resource quota for each
type NamespaceProcessor struct {
	ClusterInfoScraper *cluster.ClusterScraper
	clusterName string
	ClusterResources map[metrics.ResourceType]*repository.KubeDiscoveredResource
}

// Query the Kubernetes API Server and Get the Namespace objects
func (processor *NamespaceProcessor) ProcessNamespaces() (map[string]*repository.KubeNamespace, error) {
	namespaceList, err := processor.ClusterInfoScraper.GetNamespaces()
	if err != nil {
		return nil, fmt.Errorf("Error getting namespaces for cluster %s:%s\n", processor.clusterName, err)
	}
	glog.Infof("There are %d namespaces\n", len(namespaceList))

	quotaMap, err :=  processor.ClusterInfoScraper.GetNamespaceQuotas()
	if err != nil {
		fmt.Errorf("failed to list all quotas in the cluster %s: %s", processor.clusterName, err)
	}
	glog.Infof("There are %d resource quotas\n", len(quotaMap))

	namespaces := make(map[string]*repository.KubeNamespace)
	for _, item := range namespaceList{
		namespace := &repository.KubeNamespace{
			ClusterName: processor.clusterName,
			Name: item.Name,
		}
		var quotaEntity *repository.KubeQuota
		quotaList, hasQuota := quotaMap[item.Name]
		if !hasQuota { //Create default quota
			quotaEntity = processor.createDefaultQuota(processor.clusterName, namespace.Name,
				processor.ClusterResources)
		} else {
			quotaEntity = repository.CreateKubeQuota(processor.clusterName, namespace.Name, quotaList)
		}
		namespace.Quota = quotaEntity
		namespaces[item.Name] = namespace
	}
	return namespaces, nil
}


// Create a Quota object for namespaces that do have resource quota objects defined.
// The resource quota limits are based on the cluster compute resource limits.
func (processor *NamespaceProcessor) createDefaultQuota(clusterName, namespace string,
							clusterResources map[metrics.ResourceType]*repository.
									KubeDiscoveredResource,
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
