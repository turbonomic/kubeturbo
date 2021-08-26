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

// Get the max of resource capacity values from all container replicas. This function is used to aggregate container
// capacity values to ContainerSpec capacity.
//
// 	TODO: It's possible that main discovery happens during the rolling update of a K8s Deployment so that for the same
//	 container spc, some container replicas have previous larger resource limits/requests and others have the updated
//	 smaller values. In this case, max capacity is not latest actual value but won't cause unexpected results in Turbo
//	 server side. We could extract the correct resource limits/requests from parent controller instead of simply using max.
func GetResourceCapacity(resourceMetrics *repository.ContainerMetrics) float64 {
	capacity := 0.0
	for _, capVal := range resourceMetrics.Capacity {
		capacity = math.Max(capacity, capVal)
	}
	return capacity
}
