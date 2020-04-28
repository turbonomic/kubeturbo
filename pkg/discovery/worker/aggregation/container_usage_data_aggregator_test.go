package aggregation

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"testing"
)

var (
	cpuCommType     = proto.CommodityDTO_VCPU
	testCommodities = []*proto.CommodityDTO{
		createCommodityDTO(cpuCommType, 2.0, 1.0, 1.0),
		createCommodityDTO(cpuCommType, 4.0, 3.0, 3.0),
	}
	emptyCommodities []*proto.CommodityDTO
)

func Test_avgUsageDataAggregator_Aggregate(t *testing.T) {
	testCases := []struct {
		name                string
		aggregationStrategy string
		commodities         []*proto.CommodityDTO
		capacity            float64
		used                float64
		peak                float64
		wantErr             bool
	}{
		{
			name:                "test aggregate average usage data",
			aggregationStrategy: "average usage data strategy",
			commodities:         testCommodities,
			capacity:            3.0,
			used:                2.0,
			peak:                2.0,
			wantErr:             false,
		},
		{
			name:                "test aggregate average usage data with empty commodities",
			aggregationStrategy: "average usage data strategy",
			commodities:         emptyCommodities,
			capacity:            0.0,
			used:                0.0,
			peak:                0.0,
			wantErr:             true,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			avgUsageDataAggregator := &avgUsageDataAggregator{
				aggregationStrategy: tt.aggregationStrategy,
			}
			aggregatedCap, aggregatedUsed, aggregatedPeak, err := avgUsageDataAggregator.Aggregate(tt.commodities)
			if (err != nil) != tt.wantErr {
				t.Errorf("Aggregate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if aggregatedCap != tt.capacity {
				t.Errorf("Aggregate() aggregatedCap = %v, want %v", aggregatedCap, tt.capacity)
			}
			if aggregatedUsed != tt.used {
				t.Errorf("Aggregate() aggregatedUsed = %v, want %v", aggregatedUsed, tt.used)
			}
			if aggregatedPeak != tt.peak {
				t.Errorf("Aggregate() aggregatedPeak = %v, want %v", aggregatedPeak, tt.peak)
			}
		})
	}
}

func Test_maxUsageDataAggregator_Aggregate(t *testing.T) {
	testCases := []struct {
		name                string
		aggregationStrategy string
		commodities         []*proto.CommodityDTO
		capacity            float64
		used                float64
		peak                float64
		wantErr             bool
	}{
		{
			name:                "test aggregate max usage data",
			aggregationStrategy: "max usage data strategy",
			commodities:         testCommodities,
			capacity:            4.0,
			used:                3.0,
			peak:                3.0,
			wantErr:             false,
		},
		{
			name:                "test aggregate max usage data with empty commodities",
			aggregationStrategy: "average usage data strategy",
			commodities:         emptyCommodities,
			capacity:            0.0,
			used:                0.0,
			peak:                0.0,
			wantErr:             true,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			maxUsageDataAggregator := &maxUsageDataAggregator{
				aggregationStrategy: tt.aggregationStrategy,
			}
			aggregatedCap, aggregatedUsed, aggregatedPeak, err := maxUsageDataAggregator.Aggregate(tt.commodities)
			if (err != nil) != tt.wantErr {
				t.Errorf("Aggregate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if aggregatedCap != tt.capacity {
				t.Errorf("Aggregate() aggregatedCap = %v, want %v", aggregatedCap, tt.capacity)
			}
			if aggregatedUsed != tt.used {
				t.Errorf("Aggregate() aggregatedUsed = %v, want %v", aggregatedUsed, tt.used)
			}
			if aggregatedPeak != tt.peak {
				t.Errorf("Aggregate() aggregatedPeak = %v, want %v", aggregatedPeak, tt.peak)
			}
		})
	}
}

func createCommodityDTO(commodityType proto.CommodityDTO_CommodityType, capacity, used, peak float64) *proto.CommodityDTO {
	return &proto.CommodityDTO{
		CommodityType: &commodityType,
		Capacity:      &capacity,
		Used:          &used,
		Peak:          &peak,
	}
}
