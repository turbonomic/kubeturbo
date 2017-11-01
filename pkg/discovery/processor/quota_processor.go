package processor

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"k8s.io/client-go/pkg/api/v1"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

// Query the Kubernetes API server to get ResourceQuota objects. Parse them to create QuotaEntity.
type QuotaProcessor struct {
	clusterInfoScraper *cluster.ClusterScraper
}

// Query the Kubernetes API Server and get the Resource Quota objects per Namespace
func (processor *QuotaProcessor) ProcessResourceQuotas() map[string]*repository.KubeQuota {
	quotaList, err := processor.clusterInfoScraper.GetResourceQuotas()
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("There are %d resource quotas\n", len(quotaList))

	quotaMap := processor.collectNamespaceQuotas(quotaList)

	// Create one reconciled Quota Entity per namespace
	quotaEntityMap := make(map[string]*repository.KubeQuota)
	for namespace, _ := range quotaMap {
		reconciledQuota := processor.reconcileQuotas(namespace, quotaMap[namespace])
		quotaEntityMap[namespace] = reconciledQuota
		fmt.Printf("%s::%s\n", reconciledQuota.Name, reconciledQuota.Namespace);
	}
	return quotaEntityMap
}

func printQuota(quota *repository.KubeQuota) {
	fmt.Printf("%s::%s::%s::%s\n", quota.KubeEntity.ClusterName, quota.Namespace, quota.Name, quota.UID)
	for _, resource := range quota.ComputeResources {
		fmt.Printf("\t---> %s Capacity=%f, Used=%f\n",
			resource.Type, resource.Capacity, resource.Used)
	}
}

func (processor *QuotaProcessor) collectNamespaceQuotas(quotaList []*v1.ResourceQuota) map[string][]*v1.ResourceQuota {
	// Quota Map organized by Namespace
	quotaMap := make(map[string][]*v1.ResourceQuota)
	for _, item := range quotaList {
		quotaList, exists := quotaMap[item.Namespace]
		if !exists {
			quotaList = []*v1.ResourceQuota{}
		}
		quotaList = append(quotaList, item)
		quotaMap[item.Namespace] = quotaList
	}
	return quotaMap
}

func (processor *QuotaProcessor) reconcileQuotas(namespace string, quotas []*v1.ResourceQuota) *repository.KubeQuota {

	quota := &repository.KubeQuota{
		KubeEntity: repository.NewKubeEntity(),
	}
	finalResourceMap := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
	for _, item := range quotas {
		// Resources in each quota
		resourceStatus := item.Status
		resourceHardList := resourceStatus.Hard
		resourceUsedList := resourceStatus.Used
		for resource, _:= range resourceHardList {	//mix of allocation and object count resources
			quantity := resourceHardList[resource]
			capacityValue := quantity.MilliValue()
			used := resourceUsedList[resource]
			usedValue := used.MilliValue()

			resourceType := metrics.KubeResourceTypes[resource]
			_, exists := finalResourceMap[resourceType]
			if !exists {
				r := &repository.KubeDiscoveredResource{
					Type: metrics.KubeResourceTypes[resource],
					Capacity: float64(capacityValue),
					Used: float64(usedValue),
				}
				finalResourceMap[r.Type] = r
			}

			existingResource, _ := finalResourceMap[resourceType]
			// update the existing resource with the one in this quota
			if float64(capacityValue) < existingResource.Capacity  {
				existingResource.Capacity = float64(capacityValue)
				existingResource.Used = float64(usedValue)
			}
		}
	}

	for _, resource := range finalResourceMap {
		r := &repository.KubeDiscoveredResource{
			Type: resource.Type,
			Capacity: resource.Capacity,
			Used: resource.Used,
		}
		quota.ComputeResources[r.Type] = r
	}
	quota.ComputeResources = finalResourceMap
	quota.Name = namespace
	quota.Namespace = namespace
	quota.UID = namespace
	quota.QuotaList = quotas
	fmt.Printf("****** Reconciled Quota ===> %s\n", quota.Name)
	for _, resource := range quota.ComputeResources {
		fmt.Printf("\t ****** resource %s: cap=%f used=%f\n", resource.Type, resource.Capacity, resource.Used)
	}
	return quota
}

func (processor *QuotaProcessor) reconcileResource(newResource *repository.KubeDiscoveredResource,
							oldResource *repository.KubeDiscoveredResource) *repository.KubeDiscoveredResource {
	if newResource.Capacity < oldResource.Capacity {
		return newResource
	}
	return oldResource
}


