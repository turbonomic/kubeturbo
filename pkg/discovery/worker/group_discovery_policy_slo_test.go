package worker

import (
	"errors"
	"testing"
)

// Helper function to convert an int32 into a *int32. Golang does not support creating pointers
// against constants, so this eliminates the need to create temporary variables from which to create
// the pointers.
func pointer(value int32) *int32 {
	return &value
}

func TestValidateReplicas(t *testing.T) {
	tests := map[string]struct {
		min         *int32
		max         *int32
		expectedMin *int32
		expectedMax *int32
		expectedErr error
	}{
		"defaults": {
			min:         pointer(defaultMinReplicas),
			max:         pointer(defaultMaxReplicas),
			expectedMin: pointer(defaultMinReplicas),
			expectedMax: pointer(defaultMaxReplicas),
			expectedErr: nil,
		},
		"valid range": {
			min:         pointer(1),
			max:         pointer(10),
			expectedMin: pointer(1),
			expectedMax: pointer(10),
			expectedErr: nil,
		},
		"equal min/max": {
			min:         pointer(10),
			max:         pointer(10),
			expectedMin: pointer(10),
			expectedMax: pointer(10),
			expectedErr: nil,
		},
		"invalid minReplicas": {
			min:         pointer(0),
			max:         pointer(10000),
			expectedMin: pointer(0),
			expectedMax: pointer(10000),
			expectedErr: errors.New("ERROR"),
		},
		"invalid maxReplicas": {
			min:         pointer(1),
			max:         pointer(1000000000),
			expectedMin: pointer(1),
			expectedMax: pointer(1000000000),
			expectedErr: errors.New("ERROR"),
		},
		"min > max": {
			min:         pointer(10),
			max:         pointer(1),
			expectedMin: pointer(defaultMinReplicas),
			expectedMax: pointer(defaultMaxReplicas),
			expectedErr: nil,
		},
		"nils": {
			min:         nil,
			max:         nil,
			expectedMin: nil,
			expectedMax: nil,
			expectedErr: nil,
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			min, max, err := validateReplicas(test.min, test.max)
			if err != nil {
				if test.expectedErr == nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if min == nil && test.expectedMin != nil {
					t.Fatalf("expected %v minReplicas but found %v", test.expectedMin, min)
				}
				if min != nil && test.expectedMin != nil && *min != *test.expectedMin {
					t.Fatalf("expected %v minReplicas but found %v", test.expectedMin, *min)
				}
				if max == nil && test.expectedMax != nil {
					t.Fatalf("expected %v maxReplicas but found %v", test.expectedMax, max)
				}
				if max != nil && test.expectedMax != nil && *max != *test.expectedMax {
					t.Fatalf("expected %v maxReplicas but found %v", test.expectedMax, *max)
				}
			}
		})
	}
}

func TestIsWithinValidRange(t *testing.T) {
	tests := map[string]struct {
		replicas       int32
		expectedResult bool
	}{
		"valid replicas": {
			replicas:       10,
			expectedResult: true,
		},
		"replicas equal to min allowed": {
			replicas:       defaultMinReplicas,
			expectedResult: true,
		},
		"replicas equal to max allowed": {
			replicas:       defaultMaxReplicas,
			expectedResult: true,
		},
		"replicas below valid range": {
			replicas:       defaultMinReplicas - 1,
			expectedResult: false,
		},
		"replicas above valid range": {
			replicas:       defaultMaxReplicas + 1,
			expectedResult: false,
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			actualResult := isWithinValidRange(test.replicas)
			if actualResult != test.expectedResult {
				t.Fatalf("expected %v for %v replicas but found %v", test.expectedResult, test.replicas, actualResult)
			}
		})
	}
}
