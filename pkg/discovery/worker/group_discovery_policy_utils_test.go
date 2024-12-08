package worker

import (
	"math"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestQuantityToMilliCore(t *testing.T) {
	testCases := []struct {
		input  string
		output float32
		err    bool
	}{
		{"100m", 100, false},
		{"1", 1000, false},
		{"0.02", 20, false},
		{"-100m", -1, true},
	}

	for _, tc := range testCases {
		q, err := resource.ParseQuantity(tc.input)
		if err != nil {
			t.Fatalf("Error parsing quantity %s: %v", tc.input, err)
		}

		milliCore, err := QuantityToMilliCore(&q)

		if tc.err && err == nil {
			t.Errorf("Expected an error for input %s but got none", tc.input)
		}
		if !tc.err && err != nil {
			t.Errorf("Unexpected error for input %s: %v", tc.input, err)
		}
		if milliCore != tc.output {
			t.Errorf("Expected output %f for input %s, but got %f", tc.output, tc.input, milliCore)
		}
	}
}

func TestQuantityToMB(t *testing.T) {
	testCases := []struct {
		input  string
		output float32
		err    bool
	}{
		{"500M", 500, false},
		{"1000k", 1, false},
		{"-100M", -1, true},
		{"10G", 10000, false},
		{"1", 0.000001, false},
		{"200", 0.0002, false},
		{"500Mi", 524.29, false},
		{"1000Ki", 1.024, false},
		{"-100Mi", -1, true},
		{"10Gi", 10737.41, false},
	}

	for _, tc := range testCases {
		q, err := resource.ParseQuantity(tc.input)
		if err != nil {
			t.Fatalf("Error parsing quantity %s: %v", tc.input, err)
		}

		mb, err := QuantityToMB(&q)

		if tc.err && err == nil {
			t.Errorf("Expected an error for input %s but got none", tc.input)
		}
		if !tc.err && err != nil {
			t.Errorf("Unexpected error for input %s: %v", tc.input, err)
		}

		if math.Abs(float64(mb-tc.output)/float64(tc.output)) > 0.01 {
			t.Errorf("Expected output %f for input %s, but got %f", tc.output, tc.input, mb)
		}
	}
}

func TestParseFloatFromPercentString(t *testing.T) {
	testCases := []struct {
		input  string
		output float32
		err    bool
	}{
		{"100%", 100.0, false},
		{"50.5%", 50.5, false},
		{"-20%", -20.0, false},
		{"abc%", 0, true},
	}

	for _, tc := range testCases {
		val, err := parseFloatFromPercentString(tc.input)

		if tc.err && err == nil {
			t.Errorf("Expected an error for input %s but got none", tc.input)
		}
		if !tc.err && err != nil {
			t.Errorf("Unexpected error for input %s: %v", tc.input, err)
		}
		if val != tc.output {
			t.Errorf("Expected output %f for input %s, but got %f", tc.output, tc.input, val)
		}
	}
}

func TestRecommendOrDisable(t *testing.T) {
	testCases := []struct {
		isEnabled bool
		output    string
	}{
		{true, "RECOMMEND"},
		{false, "DISABLED"},
	}

	for _, tc := range testCases {
		result := recommendOrDisable(tc.isEnabled)
		if result != tc.output {
			t.Errorf("Expected output %s for input %t, but got %s", tc.output, tc.isEnabled, result)
		}
	}
}
