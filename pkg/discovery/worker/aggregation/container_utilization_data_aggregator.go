package aggregation

import (
	"fmt"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
	// AggregationStrategy returns aggregation strategy of this data aggregator
	AggregationStrategy() string
	// Aggregate aggregates commodities utilization data based on the given list of commodity DTOs of a commodity type
	// and aggregation strategy, and returns aggregated utilization data points.
	Aggregate(commodities []*proto.CommodityDTO) ([]float64, error)
}

// ---------------- All utilization data aggregation strategy ----------------
type allUtilizationDataAggregator struct {
	aggregationStrategy string
}

func (allDataAggregator *allUtilizationDataAggregator) AggregationStrategy() string {
	return allDataAggregator.aggregationStrategy
}

func (allDataAggregator *allUtilizationDataAggregator) Aggregate(commodities []*proto.CommodityDTO) ([]float64, error) {
	if len(commodities) == 0 {
		err := fmt.Errorf("error to aggregate commodities using %s : commodities list is empty",
			allDataAggregator.AggregationStrategy())
		return []float64{}, err
	}
	var utilizationDataPoints []float64
	for _, commodity := range commodities {
		used := *commodity.Used
		capacity := *commodity.Capacity
		if capacity == 0.0 {
			err := fmt.Errorf("error to aggregate %s commodities using %s : capacity is 0", commodity.CommodityType,
				allDataAggregator.AggregationStrategy())
			return []float64{}, err
		}
		utilization := used / capacity * 100
		utilizationDataPoints = append(utilizationDataPoints, utilization)
	}
	return utilizationDataPoints, nil
}

// ---------------- Max utilization data aggregation strategy ----------------
type maxUtilizationDataAggregator struct {
	aggregationStrategy string
}

func (maxDataAggregator *maxUtilizationDataAggregator) AggregationStrategy() string {
	return maxDataAggregator.aggregationStrategy
}

func (maxDataAggregator *maxUtilizationDataAggregator) Aggregate(commodities []*proto.CommodityDTO) ([]float64, error) {
	if len(commodities) == 0 {
		err := fmt.Errorf("error to aggregate commodities using %s : commodities list is empty",
			maxDataAggregator.AggregationStrategy())
		return []float64{}, err
	}
	maxUtilization := 0.0
	for _, commodity := range commodities {
		used := *commodity.Used
		capacity := *commodity.Capacity
		if capacity == 0.0 {
			err := fmt.Errorf("error to aggregate %s commodities using %s : capacity is 0", commodity.CommodityType,
				maxDataAggregator.AggregationStrategy())
			return []float64{}, err
		}
		utilization := used / capacity * 100
		maxUtilization = math.Max(utilization, maxUtilization)
	}
	return []float64{maxUtilization}, nil
}
