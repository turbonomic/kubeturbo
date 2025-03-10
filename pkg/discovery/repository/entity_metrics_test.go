package repository

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
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

func TestAggregateQuotaUsed(t *testing.T) {
	namespaceMetrics := NamespaceMetrics{
		Namespace: "namespace",
		QuotaUsed: map[metrics.ResourceType]float64{
			metrics.CPULimitQuota:    2.0,
			metrics.MemoryLimitQuota: 2.0,
			metrics.CPURequest:       1.0,
			metrics.MemoryRequest:    1.0,
		},
	}
	newQuotaUsed := map[metrics.ResourceType]float64{
		metrics.CPULimitQuota:    2.0,
		metrics.MemoryLimitQuota: 2.0,
		metrics.CPURequest:       1.0,
		metrics.MemoryRequest:    1.0,
	}
	expectedAggregateQuotaUsed := map[metrics.ResourceType]float64{
		metrics.CPULimitQuota:    4.0,
		metrics.MemoryLimitQuota: 4.0,
		metrics.CPURequest:       2.0,
		metrics.MemoryRequest:    2.0,
	}
	namespaceMetrics.AggregateQuotaUsed(newQuotaUsed)
	assert.Equal(t, expectedAggregateQuotaUsed, namespaceMetrics.QuotaUsed)
	if !reflect.DeepEqual(expectedAggregateQuotaUsed, namespaceMetrics.QuotaUsed) {
		t.Errorf("Test case failed: aggregateQuotaUsed:\nexpected:\n%++v\nactual:\n%++v",
			expectedAggregateQuotaUsed, namespaceMetrics.QuotaUsed)
	}
}

var nowUtc = time.Now()

func TestAggregateUsedNormal(t *testing.T) {
	namespaceMetrics := NamespaceMetrics{
		Namespace: "namespace",
		Used: map[metrics.ResourceType][]metrics.Point{
			metrics.CPU: {
				{Value: 12.8, Timestamp: nowUtc.Unix()},
				{Value: 12.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.3, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 11.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			metrics.Memory: {
				{Value: 10.5, Timestamp: nowUtc.Unix()},
				{Value: 9.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 8.0, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 12.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		},
	}
	newUsed :=
		map[metrics.ResourceType][]metrics.Point{
			metrics.CPU: {
				{Value: 2.4, Timestamp: nowUtc.Unix()},
				{Value: 2.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 3.2, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			metrics.Memory: {
				{Value: 2.0, Timestamp: nowUtc.Unix()},
				{Value: 2.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 1.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	expectedAggregateUsed :=
		map[metrics.ResourceType][]metrics.Point{
			metrics.CPU: {
				{Value: 15.2, Timestamp: nowUtc.Unix()},
				{Value: 15.0, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 15.5, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 14.0, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			metrics.Memory: {
				{Value: 12.5, Timestamp: nowUtc.Unix()},
				{Value: 12.0, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 9.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 14.8, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	namespaceMetrics.AggregateUsed(newUsed)
	if !metrics.MetricPointsMapAlmostEqual(expectedAggregateUsed, namespaceMetrics.Used) {
		t.Errorf("Test case failed: aggregateUsed:\nexpected:\n%++v\nactual:\n%++v",
			expectedAggregateUsed, namespaceMetrics.Used)
	}
}
