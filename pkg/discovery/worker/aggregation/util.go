package aggregation

import (
	"fmt"
	"math"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

func isResourceMetricsValid(resourceMetrics *repository.ContainerMetrics, dataAggregator interface{}) ([]metrics.Point, bool, error) {
	usedPoints, isRightType := resourceMetrics.Used.([]metrics.Point)
	if !isRightType {
		err := fmt.Errorf("the metrics type received for aggregation is wrong expected []metrics.Point: got: %T", resourceMetrics.Used)
		return usedPoints, false, err
	}
	if len(usedPoints) == 0 || len(resourceMetrics.Capacity) == 0 {
		return usedPoints, false, fmt.Errorf("error aggregating container data using %s: used or capacity data points list is empty", dataAggregator)
	}
	return usedPoints, true, nil
}

// Use the max of resource capacity values from all container replicas.
// CPU capacity has been converted from milli-cores to MHz. This will make the aggregated CPU capacity on container
// spec more consistent if container replicas are on different nodes with different CPU frequency.
func GetResourceCapacity(resourceMetrics *repository.ContainerMetrics) float64 {
	capacity := 0.0
	for _, capVal := range resourceMetrics.Capacity {
		capacity = math.Max(capacity, capVal)
	}
	return capacity
}
