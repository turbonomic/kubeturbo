package aggregation

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"math"
)

const (
	maxUtilizationDataStrategy                 = "maxUtilizationData"
	allUtilizationDataStrategy                 = "allUtilizationData"
	DefaultContainerUtilizationDataAggStrategy = allUtilizationDataStrategy
)

var (
	// Map from the configured utilization data aggregation strategy to utilization data aggregator
	ContainerUtilizationDataAggregators = map[string]ContainerUtilizationDataAggregator{
		maxUtilizationDataStrategy: &maxUtilizationDataAggregator{aggregationStrategy: "max utilization data strategy"},
		allUtilizationDataStrategy: &allUtilizationDataAggregator{aggregationStrategy: "all utilization data strategy"},
	}
)

// ContainerUtilizationDataAggregator interface represents a type of container utilization data aggregator
type ContainerUtilizationDataAggregator interface {
	String() string
	// Aggregate aggregates commodities utilization data based on the given aggregation strategy and ContainerMetrics with
	// capacity value and multiple usage data points, and returns aggregated utilization data points, last point timestamp
	// and duration of collecting given metrics points (difference between last timestamp and first timestamp).
	Aggregate(resourceMetrics *repository.ContainerMetrics) ([]float64, int64, int32, error)
}

// ---------------- All utilization data aggregation strategy ----------------
type allUtilizationDataAggregator struct {
	aggregationStrategy string
}

func (allDataAggregator *allUtilizationDataAggregator) String() string {
	return allDataAggregator.aggregationStrategy
}

func (allDataAggregator *allUtilizationDataAggregator) Aggregate(resourceMetrics *repository.ContainerMetrics) ([]float64, int64, int32, error) {
	if len(resourceMetrics.Used) == 0 {
		err := fmt.Errorf("error aggregating container utilization data using %s: used data points list is empty", allDataAggregator)
		return []float64{}, 0, 0, err
	}
	capacity := resourceMetrics.Capacity
	if capacity == 0.0 {
		err := fmt.Errorf("error aggregating container utilization data using %s: capacity is 0", allDataAggregator)
		return []float64{}, 0, 0, err
	}
	var utilizationDataPoints []float64
	var firstTimestamp int64
	var lastTimestamp int64
	for _, usedPoint := range resourceMetrics.Used {
		utilization := usedPoint.Value / capacity * 100
		utilizationDataPoints = append(utilizationDataPoints, utilization)
		if firstTimestamp == 0 || usedPoint.Timestamp < firstTimestamp {
			firstTimestamp = usedPoint.Timestamp
		}
		lastTimestamp = int64(math.Max(float64(lastTimestamp), float64(usedPoint.Timestamp)))
	}
	// Calculate interval between data points
	var dataInterval int32
	if len(utilizationDataPoints) > 1 {
		dataInterval = int32(lastTimestamp-firstTimestamp) / int32(len(utilizationDataPoints)-1)
	}
	return utilizationDataPoints, lastTimestamp, dataInterval, nil
}

// ---------------- Max utilization data aggregation strategy ----------------
type maxUtilizationDataAggregator struct {
	aggregationStrategy string
}

func (maxDataAggregator *maxUtilizationDataAggregator) String() string {
	return maxDataAggregator.aggregationStrategy
}

func (maxDataAggregator *maxUtilizationDataAggregator) Aggregate(resourceMetrics *repository.ContainerMetrics) ([]float64, int64, int32, error) {
	if len(resourceMetrics.Used) == 0 {
		err := fmt.Errorf("error aggregating container utilization data using %s: used data points list is empty", maxDataAggregator)
		return []float64{}, 0, 0, err
	}
	var maxUtilization float64
	var lastTimestamp int64
	capacity := resourceMetrics.Capacity
	if capacity == 0.0 {
		err := fmt.Errorf("error aggregating container utilization data using %s: capacity is 0", maxDataAggregator)
		return []float64{}, 0, 0, err
	}
	for _, usedPoint := range resourceMetrics.Used {
		utilization := usedPoint.Value / capacity * 100
		maxUtilization = math.Max(utilization, maxUtilization)
		lastTimestamp = int64(math.Max(float64(lastTimestamp), float64(usedPoint.Timestamp)))
	}
	return []float64{maxUtilization}, lastTimestamp, 0, nil
}
