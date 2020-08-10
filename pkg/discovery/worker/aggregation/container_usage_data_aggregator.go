package aggregation

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"math"
)

const (
	avgUsageDataStrategy                 = "avgUsageData"
	maxUsageDataStrategy                 = "maxUsageData"
	DefaultContainerUsageDataAggStrategy = avgUsageDataStrategy
)

var (
	// Map from the configured utilization data aggregation strategy to utilization data aggregator
	ContainerUsageDataAggregators = map[string]ContainerUsageDataAggregator{
		avgUsageDataStrategy: &avgUsageDataAggregator{aggregationStrategy: "average usage data strategy"},
		maxUsageDataStrategy: &maxUsageDataAggregator{aggregationStrategy: "max usage data strategy"},
	}
)

// ContainerUsageDataAggregator interface represents a type of container usage data aggregator
type ContainerUsageDataAggregator interface {
	String() string
	// Aggregate aggregates commodities usage data based on the given aggregation strategy and ContainerMetrics with
	// capacity value and multiple usage data points, and returns aggregated capacity, used and peak values.
	Aggregate(resourceMetrics *repository.ContainerMetrics) (float64, float64, float64, error)
}

// ---------------- Average usage data aggregation strategy ----------------
type avgUsageDataAggregator struct {
	aggregationStrategy string
}

func (avgUsageDataAggregator *avgUsageDataAggregator) String() string {
	return avgUsageDataAggregator.aggregationStrategy
}

func (avgUsageDataAggregator *avgUsageDataAggregator) Aggregate(resourceMetrics *repository.ContainerMetrics) (float64, float64, float64, error) {
	if len(resourceMetrics.Used) == 0 {
		err := fmt.Errorf("error to aggregate container usage data using %s: used data points list is empty", avgUsageDataAggregator)
		return 0.0, 0.0, 0.0, err
	}
	usedSum := 0.0
	peak := 0.0
	for _, usedPoint := range resourceMetrics.Used {
		usedSum += usedPoint.Value
		peak = math.Max(peak, usedPoint.Value)
	}
	avgUsed := usedSum / float64(len(resourceMetrics.Used))
	return resourceMetrics.Capacity, avgUsed, peak, nil
}

// ---------------- Max usage data aggregation strategy ----------------
type maxUsageDataAggregator struct {
	aggregationStrategy string
}

func (maxUsageDataAggregator *maxUsageDataAggregator) String() string {
	return maxUsageDataAggregator.aggregationStrategy
}

func (maxUsageDataAggregator *maxUsageDataAggregator) Aggregate(resourceMetrics *repository.ContainerMetrics) (float64, float64, float64, error) {
	if len(resourceMetrics.Used) == 0 {
		err := fmt.Errorf("error to aggregate container usage data using %s: used data points list is empty", maxUsageDataAggregator)
		return 0.0, 0.0, 0.0, err
	}
	maxUsed := 0.0
	peak := 0.0
	for _, usedPoint := range resourceMetrics.Used {
		maxUsed = math.Max(maxUsed, usedPoint.Value)
		peak = math.Max(peak, usedPoint.Value)
	}
	return resourceMetrics.Capacity, maxUsed, peak, nil
}
