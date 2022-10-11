package metrics

import (
	"testing"
	"time"
)

var nowUtc = time.Now()

// Normal use case: same resources and same number of metric points between the current accumulated and the new portion
// ==> simple addition
func TestAccumulateMultiPoints_Normal(t *testing.T) {
	accumulated :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 12.8, Timestamp: nowUtc.Unix()},
				{Value: 12.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.3, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 11.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 10.5, Timestamp: nowUtc.Unix()},
				{Value: 9.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 8.0, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 12.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	newPortion :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 2.4, Timestamp: nowUtc.Unix()},
				{Value: 2.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 3.2, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 2.0, Timestamp: nowUtc.Unix()},
				{Value: 2.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 1.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	expected :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 15.2, Timestamp: nowUtc.Unix()},
				{Value: 15.0, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 15.5, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 14.0, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 12.5, Timestamp: nowUtc.Unix()},
				{Value: 12.0, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 9.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 14.8, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	AccumulateMultiPoints(accumulated, newPortion)
	if !MetricPointsMapAlmostEqual(expected, accumulated) {
		t.Errorf("Test case failed: TestAccumulateMultiPoints_Normal:\nexpected:\n%++v\nactual:\n%++v",
			expected, accumulated)
	}
}

// New portion is missing one of the resources
// ==> unchanged for that resource in accumulated
func TestAccumulateMultiPoints_NewPortionMissingAResource(t *testing.T) {
	accumulated :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 12.8, Timestamp: nowUtc.Unix()},
				{Value: 12.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.3, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 11.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 10.5, Timestamp: nowUtc.Unix()},
				{Value: 9.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 8.0, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 12.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	newPortion :=
		map[ResourceType][]Point{
			Memory: {
				{Value: 2.0, Timestamp: nowUtc.Unix()},
				{Value: 2.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 1.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	expected :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 12.8, Timestamp: nowUtc.Unix()},
				{Value: 12.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.3, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 11.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 12.5, Timestamp: nowUtc.Unix()},
				{Value: 12.0, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 9.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 14.8, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	AccumulateMultiPoints(accumulated, newPortion)
	if !MetricPointsMapAlmostEqual(expected, accumulated) {
		t.Errorf("Test case failed: TestAccumulateMultiPoints_NewPortionMissingAResource:\n"+
			"expected:\n%++v\nactual:\n%++v", expected, accumulated)
	}
}

// Accumulated is missing one of the resources
// ==> new portion for that resource will be the accumulated
func TestAccumulateMultiPoints_AccumulatedMissingAResource(t *testing.T) {
	accumulated :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 12.8, Timestamp: nowUtc.Unix()},
				{Value: 12.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.3, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 11.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	newPortion :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 2.4, Timestamp: nowUtc.Unix()},
				{Value: 2.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 3.2, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 2.0, Timestamp: nowUtc.Unix()},
				{Value: 2.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 1.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	expected :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 15.2, Timestamp: nowUtc.Unix()},
				{Value: 15.0, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 15.5, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 14.0, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 2.0, Timestamp: nowUtc.Unix()},
				{Value: 2.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 1.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	AccumulateMultiPoints(accumulated, newPortion)
	if !MetricPointsMapAlmostEqual(expected, accumulated) {
		t.Errorf("Test case failed: TestAccumulateMultiPoints_AccumulatedMissingAResource:\n"+
			"expected:\n%++v\nactual:\n%++v", expected, accumulated)
	}
}

// New portion is missing some metric points for a resource
// ==> the new portion of that resource will be discarded;
//
//	the other resources without missing metric points will still be accumulated
func TestAccumulateMultiPoints_NewPortionMissingMetricPoints(t *testing.T) {
	accumulated :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 12.8, Timestamp: nowUtc.Unix()},
				{Value: 12.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.3, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 11.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 10.5, Timestamp: nowUtc.Unix()},
				{Value: 9.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 8.0, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 12.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	newPortion :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 2.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 3.2, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 2.0, Timestamp: nowUtc.Unix()},
				{Value: 2.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 1.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	expected :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 12.8, Timestamp: nowUtc.Unix()},
				{Value: 12.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.3, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 11.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 12.5, Timestamp: nowUtc.Unix()},
				{Value: 12.0, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 9.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 14.8, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	AccumulateMultiPoints(accumulated, newPortion)
	if !MetricPointsMapAlmostEqual(expected, accumulated) {
		t.Errorf("Test case failed: TestAccumulateMultiPoints_NewPortionMissingMetricPoints:\n"+
			"expected:\n%++v\nactual:\n%++v", expected, accumulated)
	}
}

// Accumulated is missing some metric points for a resource
// ==> the accumulated of that resource will be discarded and replaced by the new portion's;
//
//	the other resources without missing metric points will still be accumulated as normal
func TestAccumulateMultiPoints_AccumulatedMissingMetricPoints(t *testing.T) {
	accumulated :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 12.8, Timestamp: nowUtc.Unix()},
				{Value: 12.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.3, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 11.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 10.5, Timestamp: nowUtc.Unix()},
				{Value: 9.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 12.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	newPortion :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 2.4, Timestamp: nowUtc.Unix()},
				{Value: 2.9, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 3.2, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.5, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 2.0, Timestamp: nowUtc.Unix()},
				{Value: 2.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 1.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	expected :=
		map[ResourceType][]Point{
			CPU: {
				{Value: 15.2, Timestamp: nowUtc.Unix()},
				{Value: 15.0, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 15.5, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 14.0, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
			Memory: {
				{Value: 2.0, Timestamp: nowUtc.Unix()},
				{Value: 2.1, Timestamp: nowUtc.Add(time.Minute).Unix()},
				{Value: 1.7, Timestamp: nowUtc.Add(time.Minute * 2).Unix()},
				{Value: 2.4, Timestamp: nowUtc.Add(time.Minute * 3).Unix()},
			},
		}
	AccumulateMultiPoints(accumulated, newPortion)
	if !MetricPointsMapAlmostEqual(expected, accumulated) {
		t.Errorf("Test case failed: TestAccumulateMultiPoints_AccumulatedMissingMetricPoints:\n"+
			"expected:\n%++v\nactual:\n%++v", expected, accumulated)
	}
}
