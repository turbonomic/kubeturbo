package aggregation

import (
	"fmt"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
	// Aggregate aggregates commodities usage data based on the given list of commodity DTOs of a commodity type and
	// aggregation strategy, and returns aggregated capacity, used and peak values.
	Aggregate(containerCommodities []*proto.CommodityDTO) (float64, float64, float64, error)
}

// ---------------- Average usage data aggregation strategy ----------------
type avgUsageDataAggregator struct {
	aggregationStrategy string
}

func (avgUsageDataAggregator *avgUsageDataAggregator) String() string {
	return avgUsageDataAggregator.aggregationStrategy
}

func (avgUsageDataAggregator *avgUsageDataAggregator) Aggregate(commodities []*proto.CommodityDTO) (float64, float64, float64, error) {
	if len(commodities) == 0 {
		err := fmt.Errorf("error to aggregate commodities using %s : commodities list is empty", avgUsageDataAggregator)
		return 0.0, 0.0, 0.0, err
	}
	capacitySum := 0.0
	usedSum := 0.0
	peakSum := 0.0
	for _, commodity := range commodities {
		capacitySum += *commodity.Capacity
		usedSum += *commodity.Used
		peakSum += *commodity.Peak
	}
	avgCapacity := capacitySum / float64(len(commodities))
	avgUsed := usedSum / float64(len(commodities))
	avgPeak := peakSum / float64(len(commodities))
	return avgCapacity, avgUsed, avgPeak, nil
}

// ---------------- Max usage data aggregation strategy ----------------
type maxUsageDataAggregator struct {
	aggregationStrategy string
}

func (maxUsageDataAggregator *maxUsageDataAggregator) String() string {
	return maxUsageDataAggregator.aggregationStrategy
}

func (maxUsageDataAggregator *maxUsageDataAggregator) Aggregate(commodities []*proto.CommodityDTO) (float64, float64, float64, error) {
	if len(commodities) == 0 {
		err := fmt.Errorf("error to aggregate commodities using %s : commodities list is empty", maxUsageDataAggregator)
		return 0.0, 0.0, 0.0, err
	}
	maxCapacity := 0.0
	maxUsed := 0.0
	maxPeak := 0.0
	for _, commodity := range commodities {
		maxCapacity = math.Max(maxCapacity, *commodity.Capacity)
		maxUsed = math.Max(maxUsed, *commodity.Used)
		maxPeak = math.Max(maxPeak, *commodity.Peak)
	}
	return maxCapacity, maxUsed, maxPeak, nil
}
