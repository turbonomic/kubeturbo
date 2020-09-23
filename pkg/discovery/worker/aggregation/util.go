package aggregation

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"math"
)

func isResourceMetricsValid(resourceMetrics *repository.ContainerMetrics, dataAggregator interface{}) (bool, error) {
	if len(resourceMetrics.Used) == 0 || len(resourceMetrics.Capacity) == 0 {
		return false, fmt.Errorf("error aggregating container data using %s: used or capacity data points list is empty", dataAggregator)
	}
	return true, nil
}

// Use the max of resource capacity values from all container replicas.
// CPU capacity has been converted from milli-cores to MHz. This will make the aggregated CPU capacity on container
// spec more consistent if container replicas are on different nodes with different CPU frequency.
func getResourceCapacity(resourceMetrics *repository.ContainerMetrics) float64 {
	capacity := 0.0
	for _, capVal := range resourceMetrics.Capacity {
		capacity = math.Max(capacity, capVal)
	}
	return capacity
}
