package aggregation

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"testing"
)

var (
	testContainerMetrics = &repository.ContainerMetrics{
		Capacity: []float64{3, 4},
		Used: []metrics.Point{
			createContainerMetricPoint(1.0, 1),
			createContainerMetricPoint(3.0, 2),
			createContainerMetricPoint(2.0, 3),
		},
	}
	emptyContainerMetrics = &repository.ContainerMetrics{
		Used: []metrics.Point{},
	}
)

func Test_avgUsageDataAggregator_Aggregate(t *testing.T) {
	testCases := []struct {
		name                string
		aggregationStrategy string
		containerMetrics    *repository.ContainerMetrics
		capacity            float64
		used                float64
		peak                float64
		wantErr             bool
	}{
		{
			name:                "test aggregate average usage data",
			aggregationStrategy: "average usage data strategy",
			containerMetrics:    testContainerMetrics,
			capacity:            4.0,
			used:                2.0,
			peak:                3.0,
			wantErr:             false,
		},
		{
			name:                "test aggregate average usage data with empty commodities",
			aggregationStrategy: "average usage data strategy",
			containerMetrics:    emptyContainerMetrics,
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
			aggregatedCap, aggregatedUsed, aggregatedPeak, err := avgUsageDataAggregator.Aggregate(tt.containerMetrics)
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
		containerMetrics    *repository.ContainerMetrics
		capacity            float64
		used                float64
		peak                float64
		wantErr             bool
	}{
		{
			name:                "test aggregate max usage data",
			aggregationStrategy: "max usage data strategy",
			containerMetrics:    testContainerMetrics,
			capacity:            4.0,
			used:                3.0,
			peak:                3.0,
			wantErr:             false,
		},
		{
			name:                "test aggregate max usage data with empty commodities",
			aggregationStrategy: "average usage data strategy",
			containerMetrics:    emptyContainerMetrics,
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
			aggregatedCap, aggregatedUsed, aggregatedPeak, err := maxUsageDataAggregator.Aggregate(tt.containerMetrics)
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

func createContainerMetricPoint(value float64, timestamp int64) metrics.Point {
	return metrics.Point{
		Value:     value,
		Timestamp: timestamp,
	}
}
