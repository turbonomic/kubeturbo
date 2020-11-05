package metrics

import "math"

// Utilities to compare two floating points, their slices, and the metrics.Points, up to a certain precision
const float64EqualityThreshold = 1e-9

func floatAlmostEqual(a, b float64) bool {
	return math.Abs(a - b) <= float64EqualityThreshold
}

func metricPointsAlmostEqual(x, y []Point) bool {
	if len(x) != len(y) {
		return false
	}
	for i, xpt := range x {
		if !floatAlmostEqual(xpt.Value, y[i].Value) {
			return false
		}
	}
	return true
}

func MetricPointsMapAlmostEqual(xmap, ymap map[ResourceType][]Point) bool {
	for resourceType, xpts := range xmap {
		ypts, exists := ymap[resourceType]
		if !exists {
			return false
		}
		if !metricPointsAlmostEqual(xpts, ypts) {
			return false
		}
	}
	return true
}