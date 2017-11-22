package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// Collection of allocation metrics for a pod
type PodMetrics struct {
	PodName 	string
	QuotaName 	string
	NodeName 	string
	PodKey  string
	AllocationUsed 	map[metrics.ResourceType]float64
}

func NewPodMetrics(podName, quotaName, nodeName string) *PodMetrics {
	return &PodMetrics {
		PodName: podName,
		QuotaName: quotaName,
		NodeName: nodeName,
		AllocationUsed: make(map[metrics.ResourceType]float64),
	}
}

// Collection of allocation metrics for a node
type NodeMetrics struct {
	NodeName string
	NodeKey  string
	AllocationUsed 	map[metrics.ResourceType]float64
	AllocationCap 	map[metrics.ResourceType]float64
}

func NewNodeMetrics(nodeName string) *NodeMetrics {
	return &NodeMetrics {
		NodeName: nodeName,
		AllocationUsed: make(map[metrics.ResourceType]float64),
		AllocationCap: make(map[metrics.ResourceType]float64),
	}
}

// Collection of allocation metrics for a quota
type QuotaMetrics struct {
	QuotaName           string
	AllocationBoughtMap map[string]map[metrics.ResourceType]float64
}

func NewQuotaMetrics(nodeName string) *QuotaMetrics {
	return &QuotaMetrics {
		QuotaName: nodeName,
		AllocationBoughtMap: make(map[string]map[metrics.ResourceType]float64),
	}
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
