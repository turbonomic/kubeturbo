package aggregation

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"reflect"
	"testing"
)

func Test_allUtilizationDataAggregator_Aggregate(t *testing.T) {
	testCases := []struct {
		name                string
		aggregationStrategy string
		commodities         []*proto.CommodityDTO
		points              []float64
		wantErr             bool
	}{
		{
			name:                "test aggregate all utilization data",
			aggregationStrategy: "all utilization data strategy",
			commodities:         testCommodities,
			points:              []float64{50.0, 75.0},
			wantErr:             false,
		},
		{
			name:                "test aggregate all utilization data with empty commodities",
			aggregationStrategy: "all utilization data strategy",
			commodities:         emptyCommodities,
			points:              []float64{},
			wantErr:             true,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			allDataAggregator := &allUtilizationDataAggregator{
				aggregationStrategy: tt.aggregationStrategy,
			}
			points, err := allDataAggregator.Aggregate(tt.commodities)
			if (err != nil) != tt.wantErr {
				t.Errorf("Aggregate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(points, tt.points) {
				t.Errorf("Aggregate() got = %v, want %v", points, tt.points)
			}
		})
	}
}

func Test_maxUtilizationDataAggregator_Aggregate(t *testing.T) {
	testCases := []struct {
		name                string
		aggregationStrategy string
		commodities         []*proto.CommodityDTO
		points              []float64
		wantErr             bool
	}{
		{
			name:                "test aggregate max utilization data",
			aggregationStrategy: "max utilization data strategy",
			commodities:         testCommodities,
			points:              []float64{75.0},
			wantErr:             false,
		},
		{
			name:                "test aggregate all utilization data with empty commodities",
			aggregationStrategy: "all utilization data strategy",
			commodities:         emptyCommodities,
			points:              []float64{},
			wantErr:             true,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			maxDataAggregator := &maxUtilizationDataAggregator{
				aggregationStrategy: tt.aggregationStrategy,
			}
			points, err := maxDataAggregator.Aggregate(tt.commodities)
			if (err != nil) != tt.wantErr {
				t.Errorf("Aggregate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(points, tt.points) {
				t.Errorf("Aggregate() got = %v, want %v", points, tt.points)
			}
		})
	}
}
