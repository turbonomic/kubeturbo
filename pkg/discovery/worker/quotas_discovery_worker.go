package worker

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/golang/glog"
)

const (
	k8sQuotasWorkerID string = "ResourceQuotasDiscoveryWorker"
)

// Converts the cluster quotaEntity and QuotaMetrics objects to create Quota DTOs
type k8sResourceQuotasDiscoveryWorker struct {
	id string
	Cluster *repository.ClusterSummary
}

func Newk8sResourceQuotasDiscoveryWorker(cluster *repository.ClusterSummary,
						) *k8sResourceQuotasDiscoveryWorker{
	return &k8sResourceQuotasDiscoveryWorker{
		Cluster: cluster,
		id: k8sQuotasWorkerID,
	}
}

func (worker *k8sResourceQuotasDiscoveryWorker) Do(quotaMetricsList []*repository.QuotaMetrics,
							) ([]*proto.EntityDTO, error) {
	// Combine quota discovery results from different nodes
	quotaMetricsMap := make(map[string]*repository.QuotaMetrics)

	// combine quota metrics results from different discovery workers
	// each worker will provide the allocation bought for a set of nodes and
	// the allocation used for the pods running on those nodes
	for _, quotaMetrics := range quotaMetricsList {
		glog.V(4).Infof("%s : merging metrics for nodes %s\n",
					quotaMetrics.QuotaName, quotaMetrics.NodeProviders)
		_, exists := quotaMetricsMap[quotaMetrics.QuotaName]
		if !exists {
			quotaMetricsMap[quotaMetrics.QuotaName] = quotaMetrics
		}
		existingMetric := quotaMetricsMap[quotaMetrics.QuotaName]
		// merge the provider node metrics into the existing quota metrics
		for node, nodeMap := range quotaMetrics.AllocationBoughtMap {
			existingMetric.UpdateAllocationBought(node, nodeMap)
		}

		//merge the pod usage from this quota metrics into the existing quota metrics
		existingMetric.UpdateAllocationSold(quotaMetrics.AllocationSold)
	}

	kubeNodes := worker.Cluster.Nodes
	var nodeUIDs []string
	var totalNodeFrequency float64
	for _, node := range kubeNodes {
		nodeUIDs = append(nodeUIDs, node.UID)
		totalNodeFrequency += node.NodeCpuFrequency
	}

	averageNodeFrequency := totalNodeFrequency / float64(len(kubeNodes))
	glog.V(2).Infof("Average cluster node cpu frequency in MHz %f\n", averageNodeFrequency)

	// Create the allocation resources for all quota entities using the metrics object
	for quotaName, quotaEntity := range worker.Cluster.QuotaMap {
		// the quota metrics
		quotaMetrics, exists := quotaMetricsMap[quotaName]
		if !exists {
			glog.Errorf("%s : missing allocation metrics for quota\n", quotaName)
			continue
		}
		quotaEntity.AverageNodeCpuFrequency = averageNodeFrequency

		// create provider entity for each node
		for _, node := range kubeNodes {
			nodeUID := node.UID
			quotaEntity.AddNodeProvider(nodeUID, quotaMetrics.AllocationBoughtMap[nodeUID])
		}

		// Set for allocation sold usage if it is not available from the namespace quota objects
		for resourceType, used := range quotaMetrics.AllocationSold {
			existingResource,  _ := quotaEntity.GetResource(resourceType)
			// allocation usage is available from the namespace resource quota objects
			if existingResource.Used != 0.0 {
				glog.V(4).Infof("%s:%s : *** not change existingUsed = %f, used %f\n",
					quotaName, resourceType, existingResource.Used, used)
				continue
			}
			if used != existingResource.Used {
				glog.V(4).Infof("%s:%s : updating allocation sold usage existingUsed = %f, used %f\n",
					quotaName, resourceType, existingResource.Used, used)
				quotaEntity.SetResourceUsed(resourceType, used)
			}
		}
	}

	for _, quotaEntity := range worker.Cluster.QuotaMap {
		glog.V(4).Infof("*************** DISCOVERED quota entity %s\n", quotaEntity)
	}

	// Create DTOs for each quota entity
	quotaDtoBuilder := dtofactory.NewQuotaEntityDTOBuilder(worker.Cluster.QuotaMap)
	quotaDtos, _ := quotaDtoBuilder.BuildEntityDTOs()
	return quotaDtos, nil
}
