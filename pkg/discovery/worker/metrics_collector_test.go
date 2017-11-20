package worker

import (
	"testing"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/stretchr/testify/assert"
)
var TestAllocations = []struct {
	resourceType metrics.ResourceType
	resourceValue float64
}{
	{metrics.CPULimit, 4.0},
	{metrics.CPULimit, 3.0},
	{metrics.CPULimit, 2.0},
	{metrics.CPULimit, 1.0},
}

func TestPodMetricsListAllocationUsage(t *testing.T) {

	var podMetricsList PodMetricsList
	allocation1 := map[metrics.ResourceType]float64{
		metrics.CPULimit: 4.0,
		metrics.MemoryLimit: 4,
	}
	allocation2 := map[metrics.ResourceType]float64{
		metrics.CPULimit: 6.0,
		metrics.MemoryLimit: 6,
	}
	podMetrics1 := &repository.PodMetrics {
		PodName: "pod1",
		AllocationUsed: allocation1,
	}
	podMetrics2 := &repository.PodMetrics {
		PodName: "pod1",
		AllocationUsed: allocation2,
	}

	podMetricsList = append(podMetricsList, podMetrics1)
	podMetricsList = append(podMetricsList, podMetrics2)

	resourceMap := podMetricsList.GetAllocationUsage()
	assert.Equal(t, 10.0, resourceMap[metrics.CPULimit])
	assert.Equal(t, 10.0, resourceMap[metrics.MemoryLimit])
	assert.Equal(t, 0.0, resourceMap[metrics.CPURequest])
	assert.Equal(t, 0.0, resourceMap[metrics.MemoryRequest])
}
