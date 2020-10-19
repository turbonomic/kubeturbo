package repository

import (
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"reflect"
	"testing"
)

var TestPodAllocations = []struct {
	resourceType metrics.ResourceType
	used         float64
}{
	{metrics.CPULimitQuota, 2.0},
	{metrics.MemoryLimitQuota, 2.0},
	{metrics.CPURequest, 1.0},
	{metrics.MemoryRequest, 1.0},
}

func TestPodMetrics(t *testing.T) {
	podMetrics := NewPodMetrics("p11", "n1", "q1")
	for _, testAllocation := range TestPodAllocations {
		podMetrics.QuotaUsed[testAllocation.resourceType] = testAllocation.used
		assert.Equal(t, testAllocation.used, podMetrics.QuotaUsed[testAllocation.resourceType])
	}
}

func TestUpdateQuotaSoldUsed(t *testing.T) {
	namespaceMetrics := NamespaceMetrics{
		Namespace: "namespace",
		QuotaUsed: map[metrics.ResourceType]float64{
			metrics.CPULimitQuota:    2.0,
			metrics.MemoryLimitQuota: 2.0,
			metrics.CPURequest:       1.0,
			metrics.MemoryRequest:    1.0,
		},
	}
	newQuotaSoldUsed := map[metrics.ResourceType]float64{
		metrics.CPULimitQuota:    2.0,
		metrics.MemoryLimitQuota: 2.0,
		metrics.CPURequest:       1.0,
		metrics.MemoryRequest:    1.0,
	}
	expectedUpdatedQuotaSoldUsed := map[metrics.ResourceType]float64{
		metrics.CPULimitQuota:    4.0,
		metrics.MemoryLimitQuota: 4.0,
		metrics.CPURequest:       2.0,
		metrics.MemoryRequest:    2.0,
	}
	namespaceMetrics.AggregateQuotaUsed(newQuotaSoldUsed)
	assert.Equal(t, expectedUpdatedQuotaSoldUsed, namespaceMetrics.QuotaUsed)
	if !reflect.DeepEqual(expectedUpdatedQuotaSoldUsed, namespaceMetrics.QuotaUsed) {
		t.Errorf("Test case failed: updatedQuotaSoldUsed:\nexpected:\n%++v\nactual:\n%++v",
			expectedUpdatedQuotaSoldUsed, namespaceMetrics.QuotaUsed)
	}
}
