package util

import (
	"fmt"
	"sort"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// AccumulateMultiPoints adds the metric points from the "newPortion" to the currently "accumulated".
// Since the length of the metric point slice may be different due to pod startup and shutdown in the collection period,
// this is not a trivial addition.  For now we will just discard any slice of metric points not covering the full
// collection period.
func AccumulateMultiPoints(accumulated map[metrics.ResourceType][]metrics.Point, newPortion map[metrics.ResourceType][]metrics.Point) {
	for resourceType, podUsed := range newPortion {
		existingPoints := accumulated[resourceType]
		if len(existingPoints) < len(podUsed) {
			// Throw away existing points with a shorter length, which mostly indicates that it is from pods with
			// readings for a partial collection period (just started or shut down).  We should do a better job to
			// aggregate this kind of partial readings based on timestamps, perhaps with the knowledge of the sampling
			// periods.  But for the time being I'm just discarding them. TODO
			existingPoints = make([]metrics.Point, len(podUsed))
			copy(existingPoints, podUsed)
			accumulated[resourceType] = existingPoints
		} else if len(podUsed) < len(existingPoints) {
			// Similarly discard for now as these are readings from a partial collection period
		} else {
			for idx, point := range podUsed {
				existingPoints[idx].Value += point.Value
			}
		}
	}
}

// ConvertCumulativeToPoints converts a series of cumulative data points to a series of individual data points
func ConvertCumulativeToPoints(metricCumulative []metrics.Cumulative) ([]metrics.Point, error) {
	if len(metricCumulative) <= 1 {
		return nil, fmt.Errorf("the number of metricCumulative %v is equal or smaller than 1", len(metricCumulative))
	}
	sort.SliceStable(metricCumulative, func(i, j int) bool {
		return metricCumulative[i].Timestamp < metricCumulative[j].Timestamp
	})
	var points []metrics.Point
	for i := 0; i < len(metricCumulative)-1; i++ {
		usageCoreNanoSeconds := metricCumulative[i+1].Value - metricCumulative[i].Value
		timeIntervalNanoSeconds := MetricMilliToNano(float64(metricCumulative[i+1].Timestamp - metricCumulative[i].Timestamp))
		if usageCoreNanoSeconds >= 0 && timeIntervalNanoSeconds > 0 {
			cpuMilliCores := MetricUnitToMilli(usageCoreNanoSeconds / timeIntervalNanoSeconds)
			points = append(points, metrics.Point{
				Value:     cpuMilliCores,
				Timestamp: metricCumulative[i+1].Timestamp,
			})
		}
	}
	return points, nil
}

func CopyPoints(metricPoints []metrics.Point) []metrics.Point {
	newMetricPoints := make([]metrics.Point, len(metricPoints))
	for i := range metricPoints {
		value := metricPoints[i].Value
		newMetricPoints[i] = metrics.Point{
			Value:     value,
			Timestamp: metricPoints[i].Timestamp,
		}
	}
	return newMetricPoints
}

func CopyThrottlingCumulative(metricTCs []metrics.ThrottlingCumulative) ([][]metrics.ThrottlingCumulative, error) {
	var newMetricTCs []metrics.ThrottlingCumulative
	numberOfSamples := len(metricTCs)
	if numberOfSamples <= 1 {
		// we don't have enough samples.
		return [][]metrics.ThrottlingCumulative{}, fmt.Errorf("not enough data points [%d] to"+
			" aggregate throttling metrics", numberOfSamples)
	}
	newMetricTCs = make([]metrics.ThrottlingCumulative, numberOfSamples)
	copy(newMetricTCs, metricTCs)
	return [][]metrics.ThrottlingCumulative{newMetricTCs}, nil
}
