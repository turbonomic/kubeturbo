package worker

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"fmt"
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
	Cluster *repository.KubeCluster
}

func Newk8sResourceQuotasDiscoveryWorker(cluster *repository.KubeCluster) *k8sResourceQuotasDiscoveryWorker{
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

	// Missing nodes
	kubeNodes := worker.Cluster.Nodes
	for _, quotaMetrics := range quotaMetricsMap {
		for nodeName, _ := range kubeNodes {
			_, hasNode := quotaMetrics.AllocationBoughtMap[nodeName]
			if !hasNode {
				glog.V(4).Infof("%s : missing metrics for node %s\n", quotaMetrics.QuotaName, nodeName)
				quotaMetrics.CreateNodeMetrics(nodeName, metrics.ComputeAllocationResources)
			}
		}
	}

	for quotaName, quotaMetric := range quotaMetricsMap {
		fmt.Printf("Combined quota metrics %s\n", quotaName)
		for node, resources := range quotaMetric.AllocationBoughtMap {
			fmt.Printf("\t allocation map %s --> %++v\n", node, resources)
		}
	}
	quotaDtoBuilder := dtofactory.NewQuotaEntityDTOBuilder(worker.Cluster)
	quotaDtos, _ := quotaDtoBuilder.BuildEntityDTOs(quotaMetricsMap)
	return quotaDtos, nil
}
