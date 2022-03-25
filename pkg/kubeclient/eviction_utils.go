/*Copied from
k8s.io/kubernetes/pkg/kubelet/eviction/helpers.go
k8s.io/kubernetes/pkg/kubelet/eviction/api/types.go
*/
package kubeclient

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Signal defines a signal that can trigger eviction of pods on a node.
type Signal string

// ThresholdOperator is the operator used to express a Threshold.
type ThresholdOperator string

// ThresholdValue is a value holder that abstracts literal versus percentage based quantity
type ThresholdValue struct {
	// The following fields are exclusive. Only the topmost non-zero field is used.

	// Quantity is a quantity associated with the signal that is evaluated against the specified operator.
	Quantity *resource.Quantity
	// Percentage represents the usage percentage over the total resource that is evaluated against the specified operator.
	Percentage float32
}

// Threshold defines a metric for when eviction should occur.
type Threshold struct {
	// Signal defines the entity that was measured.
	Signal Signal
	// Operator represents a relationship of a signal to a value.
	Operator ThresholdOperator
	// Value is the threshold the resource is evaluated against.
	Value ThresholdValue
	// GracePeriod represents the amount of time that a threshold must be met before eviction is triggered.
	GracePeriod time.Duration
	// MinReclaim represents the minimum amount of resource to reclaim if the threshold is met.
	MinReclaim *ThresholdValue
}

const (
	// SignalMemoryAvailable is memory available (i.e. capacity - workingSet), in bytes.
	SignalMemoryAvailable Signal = "memory.available"
	// SignalNodeFsAvailable is amount of storage available on filesystem that kubelet uses for volumes, daemon logs, etc.
	SignalNodeFsAvailable Signal = "nodefs.available"
	// SignalNodeFsInodesFree is amount of inodes available on filesystem that kubelet uses for volumes, daemon logs, etc.
	SignalNodeFsInodesFree Signal = "nodefs.inodesFree"
	// SignalImageFsAvailable is amount of storage available on filesystem that container runtime uses for storing images and container writable layers.
	SignalImageFsAvailable Signal = "imagefs.available"
	// SignalImageFsInodesFree is amount of inodes available on filesystem that container runtime uses for storing images and container writable layers.
	SignalImageFsInodesFree Signal = "imagefs.inodesFree"
	// SignalAllocatableMemoryAvailable is amount of memory available for pod allocation (i.e. allocatable - workingSet (of pods), in bytes.
	SignalAllocatableMemoryAvailable Signal = "allocatableMemory.available"
	// SignalPIDAvailable is amount of PID available for pod allocation
	SignalPIDAvailable Signal = "pid.available"

	unsupportedEvictionSignal = "unsupported eviction signal %v"
	// inodes, number. internal to this module, used to account for local disk inode consumption.
	resourceInodes v1.ResourceName = "inodes"
	// resourcePids, number. internal to this module, used to account for local pid consumption.
	resourcePids v1.ResourceName = "pids"

	NodeAllocatableEnforcementKey = "pods"
	// OpLessThan is the operator that expresses a less than operator.
	OpLessThan ThresholdOperator = "LessThan"
)

var (
	// signalToResource maps a Signal to its associated Resource.
	signalToResource map[Signal]v1.ResourceName

	// OpForSignal maps Signals to ThresholdOperators.
	// Today, the only supported operator is "LessThan". This may change in the future,
	// for example if "consumed" (as opposed to "available") type signals are added.
	// In both cases the directionality of the threshold is implicit to the signal type
	// (for a given signal, the decision to evict will be made when crossing the threshold
	// from either above or below, never both). There is thus no reason to expose the
	// operator in the Kubelet's public API. Instead, we internally map signal types to operators.
	OpForSignal = map[Signal]ThresholdOperator{
		SignalMemoryAvailable:            OpLessThan,
		SignalNodeFsAvailable:            OpLessThan,
		SignalNodeFsInodesFree:           OpLessThan,
		SignalImageFsAvailable:           OpLessThan,
		SignalImageFsInodesFree:          OpLessThan,
		SignalAllocatableMemoryAvailable: OpLessThan,
		SignalPIDAvailable:               OpLessThan,
	}
)

func init() {
	// map signals to resources (and vice-versa)
	signalToResource = map[Signal]v1.ResourceName{}
	signalToResource[SignalMemoryAvailable] = v1.ResourceMemory
	signalToResource[SignalAllocatableMemoryAvailable] = v1.ResourceMemory
	signalToResource[SignalImageFsAvailable] = v1.ResourceEphemeralStorage
	signalToResource[SignalImageFsInodesFree] = resourceInodes
	signalToResource[SignalNodeFsAvailable] = v1.ResourceEphemeralStorage
	signalToResource[SignalNodeFsInodesFree] = resourceInodes
	signalToResource[SignalPIDAvailable] = resourcePids
}

// ParseThresholdConfig parses the flags for thresholds.
func ParseThresholdConfig(allocatableConfig []string, evictionHard, evictionSoft, evictionSoftGracePeriod, evictionMinimumReclaim map[string]string) ([]Threshold, error) {
	results := []Threshold{}
	hardThresholds, err := parseThresholdStatements(evictionHard)
	if err != nil {
		return nil, err
	}
	results = append(results, hardThresholds...)
	softThresholds, err := parseThresholdStatements(evictionSoft)
	if err != nil {
		return nil, err
	}
	gracePeriods, err := parseGracePeriods(evictionSoftGracePeriod)
	if err != nil {
		return nil, err
	}
	minReclaims, err := parseMinimumReclaims(evictionMinimumReclaim)
	if err != nil {
		return nil, err
	}
	for i := range softThresholds {
		signal := softThresholds[i].Signal
		period, found := gracePeriods[signal]
		if !found {
			return nil, fmt.Errorf("grace period must be specified for the soft eviction threshold %v", signal)
		}
		softThresholds[i].GracePeriod = period
	}
	results = append(results, softThresholds...)
	for i := range results {
		if minReclaim, ok := minReclaims[results[i].Signal]; ok {
			results[i].MinReclaim = &minReclaim
		}
	}
	for _, key := range allocatableConfig {
		if key == NodeAllocatableEnforcementKey {
			results = addAllocatableThresholds(results)
			break
		}
	}
	return results, nil
}

func addAllocatableThresholds(thresholds []Threshold) []Threshold {
	additionalThresholds := []Threshold{}
	for _, threshold := range thresholds {
		if threshold.Signal == SignalMemoryAvailable && isHardEvictionThreshold(threshold) {
			// Copy the SignalMemoryAvailable to SignalAllocatableMemoryAvailable
			additionalThresholds = append(additionalThresholds, Threshold{
				Signal:     SignalAllocatableMemoryAvailable,
				Operator:   threshold.Operator,
				Value:      threshold.Value,
				MinReclaim: threshold.MinReclaim,
			})
		}
	}
	return append(append([]Threshold{}, thresholds...), additionalThresholds...)
}

// parseThresholdStatements parses the input statements into a list of Threshold objects.
func parseThresholdStatements(statements map[string]string) ([]Threshold, error) {
	if len(statements) == 0 {
		return nil, nil
	}
	results := []Threshold{}
	for signal, val := range statements {
		result, err := parseThresholdStatement(Signal(signal), val)
		if err != nil {
			return nil, err
		}
		if result != nil {
			results = append(results, *result)
		}
	}
	return results, nil
}

// parseThresholdStatement parses a threshold statement and returns a threshold,
// or nil if the threshold should be ignored.
func parseThresholdStatement(signal Signal, val string) (*Threshold, error) {
	if !validSignal(signal) {
		return nil, fmt.Errorf(unsupportedEvictionSignal, signal)
	}
	operator := OpForSignal[signal]
	if strings.HasSuffix(val, "%") {
		// ignore 0% and 100%
		if val == "0%" || val == "100%" {
			return nil, nil
		}
		percentage, err := parsePercentage(val)
		if err != nil {
			return nil, err
		}
		if percentage < 0 {
			return nil, fmt.Errorf("eviction percentage threshold %v must be >= 0%%: %s", signal, val)
		}
		// percentage is a float and should not be greater than 1 (100%)
		if percentage > 1 {
			return nil, fmt.Errorf("eviction percentage threshold %v must be <= 100%%: %s", signal, val)
		}
		return &Threshold{
			Signal:   signal,
			Operator: operator,
			Value: ThresholdValue{
				Percentage: percentage,
			},
		}, nil
	}
	quantity, err := resource.ParseQuantity(val)
	if err != nil {
		return nil, err
	}
	if quantity.Sign() < 0 || quantity.IsZero() {
		return nil, fmt.Errorf("eviction threshold %v must be positive: %s", signal, &quantity)
	}
	return &Threshold{
		Signal:   signal,
		Operator: operator,
		Value: ThresholdValue{
			Quantity: &quantity,
		},
	}, nil
}

// parsePercentage parses a string representing a percentage value
func parsePercentage(input string) (float32, error) {
	value, err := strconv.ParseFloat(strings.TrimRight(input, "%"), 32)
	if err != nil {
		return 0, err
	}
	return float32(value) / 100, nil
}

// parseGracePeriods parses the grace period statements
func parseGracePeriods(statements map[string]string) (map[Signal]time.Duration, error) {
	if len(statements) == 0 {
		return nil, nil
	}
	results := map[Signal]time.Duration{}
	for signal, val := range statements {
		signal := Signal(signal)
		if !validSignal(signal) {
			return nil, fmt.Errorf(unsupportedEvictionSignal, signal)
		}
		gracePeriod, err := time.ParseDuration(val)
		if err != nil {
			return nil, err
		}
		if gracePeriod < 0 {
			return nil, fmt.Errorf("invalid eviction grace period specified: %v, must be a positive value", val)
		}
		results[signal] = gracePeriod
	}
	return results, nil
}

// parseMinimumReclaims parses the minimum reclaim statements
func parseMinimumReclaims(statements map[string]string) (map[Signal]ThresholdValue, error) {
	if len(statements) == 0 {
		return nil, nil
	}
	results := map[Signal]ThresholdValue{}
	for signal, val := range statements {
		signal := Signal(signal)
		if !validSignal(signal) {
			return nil, fmt.Errorf(unsupportedEvictionSignal, signal)
		}
		if strings.HasSuffix(val, "%") {
			percentage, err := parsePercentage(val)
			if err != nil {
				return nil, err
			}
			if percentage <= 0 {
				return nil, fmt.Errorf("eviction percentage minimum reclaim %v must be positive: %s", signal, val)
			}
			results[signal] = ThresholdValue{
				Percentage: percentage,
			}
			continue
		}
		quantity, err := resource.ParseQuantity(val)
		if err != nil {
			return nil, err
		}
		if quantity.Sign() < 0 {
			return nil, fmt.Errorf("negative eviction minimum reclaim specified for %v", signal)
		}
		results[signal] = ThresholdValue{
			Quantity: &quantity,
		}
	}
	return results, nil
}

// validSignal returns true if the signal is supported.
func validSignal(signal Signal) bool {
	_, found := signalToResource[signal]
	return found
}

// isHardEvictionThreshold returns true if eviction should immediately occur
func isHardEvictionThreshold(threshold Threshold) bool {
	return threshold.GracePeriod == time.Duration(0)
}
