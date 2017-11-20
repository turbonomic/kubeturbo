package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"k8s.io/client-go/pkg/api/v1"
)

// Collection of allocation metrics for a pod
type PodMetrics struct {
	Pod 		*v1.Pod
	Node		*v1.Node
	PodName 	string
	QuotaName 	string
	NodeName 	string
	PodKey  string
	AllocationUsed 	map[metrics.ResourceType]float64
}

func (metrics *PodMetrics) GetAllocationUsageMap() map[metrics.ResourceType]float64 {
	return metrics.AllocationUsed
}

// Collection of allocation metrics for a node
type NodeMetrics struct {
	NodeName string
	NodeKey  string
	AllocationUsed 	map[metrics.ResourceType]float64
	AllocationCap 	map[metrics.ResourceType]float64
}

// Collection of allocation metrics for a quota
type QuotaMetrics struct {
	QuotaName           string
	AllocationBoughtMap map[string]map[metrics.ResourceType]float64
}

func (quotaMetric *QuotaMetrics) CreateNodeMetrics(nodeUID string, expectedQuotaResources []metrics.ResourceType) {

	if quotaMetric.AllocationBoughtMap == nil {
		quotaMetric.AllocationBoughtMap = make(map[string]map[metrics.ResourceType]float64)
	}
	emptyMap := make(map[metrics.ResourceType]float64)
	for _, rt := range expectedQuotaResources {
		emptyMap[rt] = 0.0
	}
	quotaMetric.AllocationBoughtMap[nodeUID] = emptyMap
}

func CreateDefaultQuotaMetrics(quotaName string, nodeUIDs []string) *QuotaMetrics {
	quotaMetrics := &QuotaMetrics{
		QuotaName: quotaName,
		AllocationBoughtMap: make(map[string]map[metrics.ResourceType]float64),
	}
	for _, nodeUID := range nodeUIDs {
		quotaMetrics.CreateNodeMetrics(nodeUID, metrics.ComputeAllocationResources)
	}
	return quotaMetrics
}

// -----------------------------------------------------------
type UsedMetrics map[metrics.ResourceType]float64
type CapacityMetrics map[metrics.ResourceType]float64
type ReservationMetrics map[metrics.ResourceType]float64
