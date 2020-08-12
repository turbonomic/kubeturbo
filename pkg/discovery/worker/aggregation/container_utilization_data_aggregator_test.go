package aggregation

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"reflect"
	"testing"
)

func Test_allUtilizationDataAggregator_Aggregate(t *testing.T) {
	testCases := []struct {
		name                string
		aggregationStrategy string
		containerMetrics    *repository.ContainerMetrics
		points              []float64
		lastPointTimestamp  int64
		dataInterval        int32
		wantErr             bool
	}{
		{
			name:                "test aggregate all utilization data",
			aggregationStrategy: "all utilization data strategy",
			containerMetrics:    testContainerMetrics,
			points:              []float64{25.0, 75.0, 50.0},
			lastPointTimestamp:  3,
			dataInterval:        1,
			wantErr:             false,
		},
		{
			name:                "test aggregate all utilization data with empty commodities",
			aggregationStrategy: "all utilization data strategy",
			containerMetrics:    emptyContainerMetrics,
			points:              []float64{},
			lastPointTimestamp:  0,
			dataInterval:        0,
			wantErr:             true,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			allDataAggregator := &allUtilizationDataAggregator{
				aggregationStrategy: tt.aggregationStrategy,
			}
			points, lastPointTimestamp, dataInterval, err := allDataAggregator.Aggregate(tt.containerMetrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("Aggregate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(points, tt.points) {
				t.Errorf("Aggregate() got = %v, want %v", points, tt.points)
			}
			if lastPointTimestamp != tt.lastPointTimestamp {
				t.Errorf("Aggregate() lastPointTimestamp = %v, want %v", lastPointTimestamp, tt.lastPointTimestamp)
			}
			if dataInterval != tt.dataInterval {
				t.Errorf("Aggregate() dataInterval = %v, want %v", dataInterval, tt.dataInterval)
			}
		})
	}
}

func Test_maxUtilizationDataAggregator_Aggregate(t *testing.T) {
	testCases := []struct {
		name                string
		aggregationStrategy string
		containerMetrics    *repository.ContainerMetrics
		points              []float64
		lastPointTimestamp  int64
		dataInterval        int32
		wantErr             bool
	}{
		{
			name:                "test aggregate max utilization data",
			aggregationStrategy: "max utilization data strategy",
			containerMetrics:    testContainerMetrics,
			points:              []float64{75.0},
			lastPointTimestamp:  3,
			dataInterval:        0,
			wantErr:             false,
		},
		{
			name:                "test aggregate all utilization data with empty commodities",
			aggregationStrategy: "all utilization data strategy",
			containerMetrics:    emptyContainerMetrics,
			points:              []float64{},
			lastPointTimestamp:  0,
			dataInterval:        0,
			wantErr:             true,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			maxDataAggregator := &maxUtilizationDataAggregator{
				aggregationStrategy: tt.aggregationStrategy,
			}
			points, lastPointTimestamp, dataInterval, err := maxDataAggregator.Aggregate(tt.containerMetrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("Aggregate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(points, tt.points) {
				t.Errorf("Aggregate() got = %v, want %v", points, tt.points)
			}
			if lastPointTimestamp != tt.lastPointTimestamp {
				t.Errorf("Aggregate() lastPointTimestamp = %v, want %v", lastPointTimestamp, tt.lastPointTimestamp)
			}
			if dataInterval != tt.dataInterval {
				t.Errorf("Aggregate() dataInterval = %v, want %v", dataInterval, tt.dataInterval)
			}
		})
	}
}
