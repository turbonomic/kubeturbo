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
	// capacity value and multiple usage data points. This returns aggregated utilization data points, last point timestamp
	// in milliseconds and calculated interval between 2 data points in milliseconds.
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
	isValid, err := isResourceMetricsValid(resourceMetrics, allDataAggregator)
	if !isValid || err != nil {
		return []float64{}, 0, 0, err
	}
	capacity := getResourceCapacity(resourceMetrics)
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
		if usedPoint.Timestamp > lastTimestamp {
			lastTimestamp = usedPoint.Timestamp
		}
	}
	// Calculate interval between data points
	var dataIntervalMs int32
	if len(utilizationDataPoints) > 1 {
		dataIntervalMs = int32(lastTimestamp-firstTimestamp) / int32(len(utilizationDataPoints)-1)
	}
	return utilizationDataPoints, lastTimestamp, dataIntervalMs, nil
}

// ---------------- Max utilization data aggregation strategy ----------------
type maxUtilizationDataAggregator struct {
	aggregationStrategy string
}

func (maxDataAggregator *maxUtilizationDataAggregator) String() string {
	return maxDataAggregator.aggregationStrategy
}

func (maxDataAggregator *maxUtilizationDataAggregator) Aggregate(resourceMetrics *repository.ContainerMetrics) ([]float64, int64, int32, error) {
	isValid, err := isResourceMetricsValid(resourceMetrics, maxDataAggregator)
	if !isValid || err != nil {
		return []float64{}, 0, 0, err
	}
	capacity := getResourceCapacity(resourceMetrics)
	if capacity == 0.0 {
		err := fmt.Errorf("error aggregating container utilization data using %s: capacity is 0", maxDataAggregator)
		return []float64{}, 0, 0, err
	}
	var maxUtilization float64
	var lastTimestamp int64
	for _, usedPoint := range resourceMetrics.Used {
		utilization := usedPoint.Value / capacity * 100
		maxUtilization = math.Max(utilization, maxUtilization)
		if usedPoint.Timestamp > lastTimestamp {
			lastTimestamp = usedPoint.Timestamp
		}
	}
	return []float64{maxUtilization}, lastTimestamp, 0, nil
}
