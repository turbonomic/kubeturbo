package metrics

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEntityMetricSink_UpdateMetricEntry(t *testing.T) {
	maxMetricPointsSize := 3
	sink := NewEntityMetricSink().WithMaxMetricPointsSize(maxMetricPointsSize)

	containerId := "containerId"
	for i := 0; i < 5; i++ {
		containerResMetric := NewEntityResourceMetric(ContainerType, containerId, Memory, Used,
			[]Point{{
				Value:     float64(i + 1),
				Timestamp: int64(i),
			}})
		sink.UpdateMetricEntry(containerResMetric)
	}

	containerMid := GenerateEntityResourceMetricUID(ContainerType, containerId, Memory, Used)
	metric, _ := sink.GetMetric(containerMid)
	metricPoints := metric.GetValue().([]Point)
	expectedPoints := []Point{
		{
			Value:     3,
			Timestamp: 2,
		},
		{
			Value:     4,
			Timestamp: 3,
		},
		{
			Value:     5,
			Timestamp: 4,
		},
	}
	assert.EqualValues(t, maxMetricPointsSize, len(metricPoints))
	assert.EqualValues(t, expectedPoints, metricPoints)
}

// Test that with cumulative data points, we retain maxMetricPointsSize+1 samples
func TestEntityMetricSink_UpdateCumulativeMetricEntry(t *testing.T) {
	maxMetricPointsSize := 3
	sink := NewEntityMetricSink().WithMaxMetricPointsSize(maxMetricPointsSize)
	containerId := "containerId"
	for i := 0; i < 5; i++ {
		containerResMetric := NewEntityResourceMetric(ContainerType, containerId, CPU, Used,
			[]Cumulative{{
				Value:     float64(i + 1),
				Timestamp: int64(i),
			}})
		sink.UpdateMetricEntry(containerResMetric)
	}

	containerMid := GenerateEntityResourceMetricUID(ContainerType, containerId, CPU, Used)
	metric, _ := sink.GetMetric(containerMid)
	metricPoints := metric.GetValue().([]Cumulative)
	expectedPoints := []Cumulative{
		{
			Value:     2,
			Timestamp: 1,
		},
		{
			Value:     3,
			Timestamp: 2,
		},
		{
			Value:     4,
			Timestamp: 3,
		},
		{
			Value:     5,
			Timestamp: 4,
		},
	}
	assert.EqualValues(t, maxMetricPointsSize+1, len(metricPoints))
	assert.EqualValues(t, expectedPoints, metricPoints)
}
