package metrics

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEntityMetricSink_UpdateMetricEntry(t *testing.T) {
	maxMetricPointsSize := 10
	sink := NewEntityMetricSink().WithMaxMetricPointsSize(maxMetricPointsSize)

	containerId := "containerId"
	for i := 0; i < 20; i++ {
		containerResMetric := NewEntityResourceMetric(ContainerType, containerId, Memory, Used,
			Points{
				Values:    []float64{float64(i)},
				Timestamp: int64(i),
			})
		sink.UpdateMetricEntry(containerResMetric)
	}

	containerMid := GenerateEntityResourceMetricUID(ContainerType, containerId, Memory, Used)
	metric, _ := sink.GetMetric(containerMid)
	metricVal := metric.GetValue().(Points)
	expectedPoints := []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	assert.EqualValues(t, maxMetricPointsSize, len(metricVal.Values))
	assert.EqualValues(t, expectedPoints, metricVal.Values)
	assert.EqualValues(t, 19, metricVal.Timestamp)
}
