package worker

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
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

func Newk8sResourceQuotasDiscoveryWorker(cluster *repository.ClusterSummary) *k8sResourceQuotasDiscoveryWorker{
	return &k8sResourceQuotasDiscoveryWorker{
		Cluster: cluster,
		id: k8sQuotasWorkerID,
	}
}

func (worker *k8sResourceQuotasDiscoveryWorker) Do(quotaMetricsList []*repository.QuotaMetrics) ([]*proto.EntityDTO, error) {
	// Combine quota discovery results from different nodes
	quotaMetricsMap := make(map[string]*repository.QuotaMetrics)

	// combine quota metrics results from different workers
	for _, quotaMetrics := range quotaMetricsList {
		_, exists := quotaMetricsMap[quotaMetrics.QuotaName]
		if !exists {
			quotaMetricsMap[quotaMetrics.QuotaName] = quotaMetrics
		}
		existingMetric := quotaMetricsMap[quotaMetrics.QuotaName]
		for node, nodeMap := range quotaMetrics.AllocationBoughtMap {
			existingMetric.AllocationBoughtMap[node] = nodeMap
		}
	}

	kubeNodes := worker.Cluster.Nodes
	var nodeUIDs []string
	for _, node := range kubeNodes {
		nodeUIDs = append(nodeUIDs, node.UID)
	}

	// Create the allocation resources for all quota entities using the metrics object
	// If the metrics is not collected for a
	for quotaName, quotaEntity := range worker.Cluster.QuotaMap {
		quotaMetrics, exists := quotaMetricsMap[quotaName]
		// add default allocation metrics if not found
		if !exists {
			// metrics will not be created for a quota if there are pods running
			// in the namespace, so create a default metrics
			glog.V(4).Infof("%s : missing allocation metrics for quota\n", quotaName)
			quotaMetrics = repository.CreateDefaultQuotaMetrics(quotaName, nodeUIDs)
		}

		// create provider entity for each node
		for _, node := range kubeNodes {
			nodeUID := node.UID
			_, hasNode := quotaMetrics.AllocationBoughtMap[nodeUID]
			// add empty allocation usage metrics for missing node providers
			if !hasNode {
				glog.V(4).Infof("%s : missing metrics for node %s\n", quotaMetrics.QuotaName, node.Name)
				quotaMetrics.CreateNodeMetrics(nodeUID, metrics.ComputeAllocationResources)
			}
			allocationMap, _ := quotaMetrics.AllocationBoughtMap[nodeUID]
			quotaEntity.AddNodeProvider(nodeUID, allocationMap)
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
