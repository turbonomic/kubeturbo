package aggregation

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	maxUtilizationDataStrategy = "maxUtilizationData"
	allUtilizationDataStrategy = "allUtilizationData"

	DefaultContainerUtilizationDataAggStrategy = allUtilizationDataStrategy

	// Map from the configured utilization data aggregation strategy to utilization data aggregator
	ContainerUtilizationDataAggregators = map[string]ContainerUtilizationDataAggregator{
		maxUtilizationDataStrategy: &maxUtilizationDataAggregator{aggregationStrategy: "max utilization data strategy"},
		allUtilizationDataStrategy: &allUtilizationDataAggregator{aggregationStrategy: "all utilization data strategy"},
	}
)

// ContainerUtilizationDataAggregator interface represents a type of container utilization data aggregator
type ContainerUtilizationDataAggregator interface {
	// AggregationStrategy returns aggregation strategy of this data aggregator
	AggregationStrategy() string
	// Aggregate aggregates commodities utilization data based on the given list of commodity DTOs of a commodity type
	// and aggregation strategy, and returns aggregated utilization data which contains utilization data points, last
	// point timestamp milliseconds and interval milliseconds
	Aggregate(commodities []*proto.CommodityDTO, lastPointTimestampMs int64) ([]float64, int64, int32, error)
}

// ---------------- All utilization data aggregation strategy ----------------
type allUtilizationDataAggregator struct {
	aggregationStrategy string
}

func (allDataAggregator *allUtilizationDataAggregator) AggregationStrategy() string {
	return allDataAggregator.aggregationStrategy
}

func (allDataAggregator *allUtilizationDataAggregator) Aggregate(commodities []*proto.CommodityDTO,
	lastPointTimestampMs int64) ([]float64, int64, int32, error) {
	// TODO aggregate all utilization data
	return []float64{}, 0, 0, nil
}

// ---------------- Max utilization data aggregation strategy ----------------
type maxUtilizationDataAggregator struct {
	aggregationStrategy string
}

func (maxDataAggregator *maxUtilizationDataAggregator) AggregationStrategy() string {
	return maxDataAggregator.aggregationStrategy
}

func (maxDataAggregator *maxUtilizationDataAggregator) Aggregate(commodities []*proto.CommodityDTO,
	lastPointTimestampMs int64) ([]float64, int64, int32, error) {
	// TODO aggregate max utilization data
	return []float64{}, 0, 0, nil
}
