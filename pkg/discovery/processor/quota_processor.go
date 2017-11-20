package processor

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"k8s.io/client-go/pkg/api/v1"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/golang/glog"
)

// Class to query the multiple resource quota objects data from the Kubernetes API server
// and create a single KubeQuota entity per namespace.
type QuotaProcessor struct {
	clusterInfoScraper *cluster.ClusterScraper
	clusterName string
}

// Query the Kubernetes API Server and create a Resource Quota object per Namespace.
func (processor *QuotaProcessor) ProcessResourceQuotas() map[string]*repository.KubeQuota {
	quotaList, err := processor.clusterInfoScraper.GetResourceQuotas()
	if err != nil {
		panic(err.Error())
	}
	glog.Infof("There are %d resource quotas\n", len(quotaList))

	// map containing namespace and the list of quotas defined in the namesapce
	quotaMap := processor.collectNamespaceQuotas(quotaList)

	// Create one reconciled Quota Entity per namespace
	quotaEntityMap := make(map[string]*repository.KubeQuota)
	for namespace, _ := range quotaMap {
		reconciledQuota := processor.reconcileQuotas(namespace, quotaMap[namespace])
		quotaEntityMap[namespace] = reconciledQuota
	}
	return quotaEntityMap
}

// Return a map containing namespace and the list of quotas defined in the namespace.
func (processor *QuotaProcessor) collectNamespaceQuotas(
				quotaList []*v1.ResourceQuota) map[string][]*v1.ResourceQuota {
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

// Return a single quota entity for the namespace by combining the quota limits for various
// resources from multiple quota objects defined in the namesapce.
// Multiple quota limits for the same resource, if any, are reconciled by selecting the
// most restrictive limit value.
func (processor *QuotaProcessor) reconcileQuotas(
			namespace string, quotas []*v1.ResourceQuota) *repository.KubeQuota {

	quota := repository.NewKubeQuota(processor.clusterName, namespace)
	quota.QuotaList = quotas
	quotaListStr := ""
	// Quota resources by collecting resources from the list of resource quota objects
	finalResourceMap := make(map[metrics.ResourceType]*repository.KubeDiscoveredResource)
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

			_, exists := finalResourceMap[resourceType]
			if !exists {
				r := &repository.KubeDiscoveredResource{
					Type: resourceType,
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
	quota.AllocationResources = finalResourceMap
	glog.Infof("Reconciled Quota %s from ===> %s \n", quota.Name, quotaListStr)
	return quota
}

func (processor *QuotaProcessor) reconcileResource(newResource *repository.KubeDiscoveredResource,
				oldResource *repository.KubeDiscoveredResource) *repository.KubeDiscoveredResource {
	if newResource.Capacity < oldResource.Capacity {
		return newResource
	}
	return oldResource
}


