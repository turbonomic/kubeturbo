package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"testing"
	"github.com/stretchr/testify/assert"
)

var TestPodAllocations= []struct {
	resourceType metrics.ResourceType
	used   float64
}{
	{metrics.CPULimit, 2.0},
	{metrics.MemoryLimit, 2.0},
	{metrics.CPURequest, 1.0},
	{metrics.MemoryRequest, 1.0},
}

var TestNodeAllocations= []struct {
	resourceType metrics.ResourceType
	capacity  float64
	used float64

}{
	{metrics.CPULimit, 2.0, 1.0},
	{metrics.MemoryLimit, 2.0, 1.0},
	{metrics.CPURequest, 1.0, 1.0},
	{metrics.MemoryRequest, 1.0, 1.0},
}

var TestQuotaAllocations= []struct {
	nodeName string
	resourceType metrics.ResourceType
	used float64

}{
	{"node1", metrics.CPULimit, 1.0},
	{"node2", metrics.CPULimit, 2.0},
	{"node3", metrics.CPULimit, 3.0},
}

func TestPodMetrics(t *testing.T) {
	podMetrics := NewPodMetrics("p11", "n1", "q1")
	for _, testAllocation := range TestPodAllocations {
		podMetrics.AllocationUsed[testAllocation.resourceType] = testAllocation.used
		assert.Equal(t,  testAllocation.used, podMetrics.AllocationUsed[testAllocation.resourceType])
	}
}


func TestNodeMetrics(t *testing.T) {
	nodeMetrics := NewNodeMetrics("node1")
	for _, testAllocation := range TestNodeAllocations {
		nodeMetrics.AllocationUsed[testAllocation.resourceType] = testAllocation.used
		nodeMetrics.AllocationCap[testAllocation.resourceType] = testAllocation.capacity
		assert.Equal(t,  testAllocation.used, nodeMetrics.AllocationUsed[testAllocation.resourceType])
		assert.Equal(t,  testAllocation.capacity, nodeMetrics.AllocationCap[testAllocation.resourceType])
	}
}

func TestQuotaMetrics(t *testing.T) {
	quotaMetrics := NewQuotaMetrics("quota1")
	for _, testAllocation := range TestQuotaAllocations {
		node := testAllocation.nodeName
		_, exists := quotaMetrics.AllocationBoughtMap[node]
		if !exists {
			quotaMetrics.AllocationBoughtMap[node] = make(map[metrics.ResourceType]float64)
		}
		nodeMap, _ := quotaMetrics.AllocationBoughtMap[node]
		nodeMap[testAllocation.resourceType] = testAllocation.used
	}

	for _, testAllocation := range TestQuotaAllocations {
		node := testAllocation.nodeName
		_, exists := quotaMetrics.AllocationBoughtMap[node]
		assert.Equal(t, true, exists)
		nodeMap, _ := quotaMetrics.AllocationBoughtMap[node]
		assert.Equal(t,  testAllocation.used, nodeMap[testAllocation.resourceType])
	}
}
