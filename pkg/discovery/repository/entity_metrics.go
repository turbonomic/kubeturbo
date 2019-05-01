package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// Collection of allocation resources bought by a pod
type PodMetrics struct {
	PodName   string
	QuotaName string
	NodeName  string
	PodKey    string
	// Compute resource used and capacity
	ComputeUsed     map[metrics.ResourceType]float64
	ComputeCapacity map[metrics.ResourceType]float64
	// Amount of Allocation resources bought from its resource quota provider
	AllocationBought map[metrics.ResourceType]float64
}

func NewPodMetrics(podName, quotaName, nodeName string) *PodMetrics {
	return &PodMetrics{
		PodName:          podName,
		QuotaName:        quotaName,
		NodeName:         nodeName,
		AllocationBought: make(map[metrics.ResourceType]float64),
		ComputeUsed:      make(map[metrics.ResourceType]float64),
		ComputeCapacity:  make(map[metrics.ResourceType]float64),
	}
}

// Collection of allocation resources sold by a node
type NodeMetrics struct {
	NodeName string
	NodeKey  string
	// Amount of Allocation resources used by different quotas
	AllocationUsed map[metrics.ResourceType]float64
	// Amount of Allocation resources sold to all quotas
	AllocationCap map[metrics.ResourceType]float64
}

func NewNodeMetrics(nodeName string) *NodeMetrics {
	return &NodeMetrics{
		NodeName:       nodeName,
		AllocationUsed: make(map[metrics.ResourceType]float64),
		AllocationCap:  make(map[metrics.ResourceType]float64),
	}
}

// Collection of allocation resources bought by a quota
type QuotaMetrics struct {
	QuotaName string
	// Amount of allocation resources bought from each node provider
	AllocationBoughtMap map[string]map[metrics.ResourceType]float64
	// Amount of allocation resources used by the pods running in the quota
	AllocationSoldUsed map[metrics.ResourceType]float64
	NodeProviders      []string
}

func NewQuotaMetrics(nodeName string) *QuotaMetrics {
	return &QuotaMetrics{
		QuotaName:           nodeName,
		AllocationBoughtMap: make(map[string]map[metrics.ResourceType]float64),
		AllocationSoldUsed:  make(map[metrics.ResourceType]float64),
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
		quotaMetrics.AllocationSoldUsed[allocationResource] = 0.0
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

func (quotaMetrics *QuotaMetrics) UpdateAllocationSoldUsed(allocationSoldUsed map[metrics.ResourceType]float64) {
	if quotaMetrics.AllocationSoldUsed == nil {
		quotaMetrics.AllocationSoldUsed = make(map[metrics.ResourceType]float64)
	}
	for resourceType, used := range allocationSoldUsed {
		var totalUsed float64
		currentUsed, exists := quotaMetrics.AllocationSoldUsed[resourceType]
		if !exists {
			totalUsed = used
		} else {
			totalUsed = currentUsed + used
		}
		quotaMetrics.AllocationSoldUsed[resourceType] = totalUsed
	}
}
