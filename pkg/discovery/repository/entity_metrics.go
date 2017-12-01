package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// Collection of allocation resources bought by a pod
type PodMetrics struct {
	PodName          string
	QuotaName        string
	NodeName         string
	PodKey           string
	// Compute resource used and capacity
	ComputeUsed 	map[metrics.ResourceType]float64
	ComputeCapacity map[metrics.ResourceType]float64
	// Amount of Allocation resources bought from its resource quota provider
	AllocationBought map[metrics.ResourceType]float64
}

func NewPodMetrics(podName, quotaName, nodeName string) *PodMetrics {
	return &PodMetrics {
		PodName: podName,
		QuotaName: quotaName,
		NodeName: nodeName,
		AllocationBought: make(map[metrics.ResourceType]float64),
		ComputeUsed: make(map[metrics.ResourceType]float64),
		ComputeCapacity: make(map[metrics.ResourceType]float64),
	}
}

// Collection of allocation resources sold by a node
type NodeMetrics struct {
	NodeName string
	NodeKey  string
	// Amount of Allocation resources used by different quotas
	AllocationUsed 	map[metrics.ResourceType]float64
	// Amount of Allocation resources sold to all quotas
	AllocationCap 	map[metrics.ResourceType]float64
}

func NewNodeMetrics(nodeName string) *NodeMetrics {
	return &NodeMetrics {
		NodeName: nodeName,
		AllocationUsed: make(map[metrics.ResourceType]float64),
		AllocationCap: make(map[metrics.ResourceType]float64),
	}
}

// Collection of allocation resources bought by a quota
type QuotaMetrics struct {
	QuotaName           string
	// Amount of allocation resources bought from each node provider
	AllocationBoughtMap map[string]map[metrics.ResourceType]float64
	// Amount of allocation resources used by the pods running in the quota
	AllocationSold map[metrics.ResourceType]float64
	NodeProviders []string
}

func NewQuotaMetrics(nodeName string) *QuotaMetrics {
	return &QuotaMetrics {
		QuotaName: nodeName,
		AllocationBoughtMap: make(map[string]map[metrics.ResourceType]float64),
		AllocationSold: make(map[metrics.ResourceType]float64),
	}
}

func CreateDefaultQuotaMetrics(quotaName string, nodeUIDs []string) *QuotaMetrics {
	quotaMetrics := NewQuotaMetrics(quotaName)
	// allocations bought from node providers
	for _, nodeUID := range nodeUIDs {
		emptyMap := make(map[metrics.ResourceType]float64)
		for _, rt := range metrics.ComputeAllocationResources {
			emptyMap[rt] = 0.0
		}
		quotaMetrics.AllocationBoughtMap[nodeUID] = emptyMap
	}
	// allocations sold
	for _, allocationResource := range metrics.ComputeAllocationResources {
		quotaMetrics.AllocationSold[allocationResource] = 0.0
	}
	return quotaMetrics
}

func (quotaMetrics *QuotaMetrics) UpdateAllocationBought(nodeUID string, allocationBought map[metrics.ResourceType]float64) {
	if quotaMetrics.AllocationBoughtMap == nil {
		quotaMetrics.AllocationBoughtMap = make(map[string]map[metrics.ResourceType]float64)
	}
	if nodeUID != "" {
		quotaMetrics.AllocationBoughtMap[nodeUID] = allocationBought
	}
}

func (quotaMetrics *QuotaMetrics) UpdateAllocationSold(allocationSold map[metrics.ResourceType]float64) {
	if quotaMetrics.AllocationSold == nil {
		quotaMetrics.AllocationSold = make(map[metrics.ResourceType]float64)
	}
	for resourceType, used := range allocationSold {
		var totalUsed float64
		currentUsed, exists := quotaMetrics.AllocationSold[resourceType]
		if !exists {
			totalUsed = used
		} else {
			totalUsed = currentUsed + used
		}
		quotaMetrics.AllocationSold[resourceType] = totalUsed
	}
}

// -----------------------------------------------------------
type UsedMetrics map[metrics.ResourceType]float64
type CapacityMetrics map[metrics.ResourceType]float64
type ReservationMetrics map[metrics.ResourceType]float64
