package aggregation

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	avgUsageDataStrategy = "avgUsageData"
	maxUsageDataStrategy = "maxUsageData"

	DefaultContainerUsageDataAggStrategy = avgUsageDataStrategy

	// Map from the configured utilization data aggregation strategy to utilization data aggregator
	ContainerUsageDataAggregators = map[string]ContainerUsageDataAggregator{
		avgUsageDataStrategy: &avgUsageDataAggregator{aggregationStrategy: "average usage data strategy"},
		maxUsageDataStrategy: &maxUsageDataAggregator{aggregationStrategy: "max usage data strategy"},
	}
)

// ContainerUsageDataAggregator interface represents a type of container usage data aggregator
type ContainerUsageDataAggregator interface {
	// AggregationStrategy returns aggregation strategy of this data aggregator
	AggregationStrategy() string
	// Aggregate aggregates commodities usage data based on the given list of commodity DTOs of a commodity type and
	// aggregation strategy, and returns aggregated capacity, used and peak values.
	Aggregate(containerCommodities []*proto.CommodityDTO) (float64, float64, float64)
}

// ---------------- Average usage data aggregation strategy ----------------
type avgUsageDataAggregator struct {
	aggregationStrategy string
}

func (avgUsageDataAggregator *avgUsageDataAggregator) AggregationStrategy() string {
	return avgUsageDataAggregator.aggregationStrategy
}

func (avgUsageDataAggregator *avgUsageDataAggregator) Aggregate(commodities []*proto.CommodityDTO) (float64, float64, float64) {
	// TODO aggregate average utilization data
	return 0.0, 0.0, 0.0
}

// ---------------- Max usage data aggregation strategy ----------------
type maxUsageDataAggregator struct {
	aggregationStrategy string
}

func (maxUsageDataAggregator *maxUsageDataAggregator) AggregationStrategy() string {
	return maxUsageDataAggregator.aggregationStrategy
}

func (maxUsageDataAggregator *maxUsageDataAggregator) Aggregate(commodities []*proto.CommodityDTO) (float64, float64, float64) {
	// TODO aggregate max utilization data
	return 0.0, 0.0, 0.0
}
