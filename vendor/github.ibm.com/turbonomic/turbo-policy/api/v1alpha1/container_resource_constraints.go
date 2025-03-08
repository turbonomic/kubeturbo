package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
)

// LimitResourceConstraint defines the resize constraint for CPU limit or Memory Limit
type LimitResourceConstraint struct {
	Max               *resource.Quantity `json:"max,omitempty"`
	Min               *resource.Quantity `json:"min,omitempty"`
	RecommendAboveMax *bool              `json:"recommendAboveMax,omitempty"`
	RecommendBelowMin *bool              `json:"recommendBelowMin,omitempty"`
}

// LimitResourceConstraints defines the resource constraints for CPU limit and Memory limit.
type LimitResourceConstraints struct {
	CPU    *LimitResourceConstraint `json:"cpu,omitempty"`
	Memory *LimitResourceConstraint `json:"memory,omitempty"`
}

// RequestResourceConstraint defines the resize constraint for CPU request and Memory quest.
// For now Turbo only generate resize down for CPU and Memory request.
type RequestResourceConstraint struct {
	Min               *resource.Quantity `json:"min,omitempty"`
	RecommendBelowMin *bool              `json:"recommendBelowMin,omitempty"`
}

// RequestResourceConstraints defines the resource constraints for CPU request and Memory request
type RequestResourceConstraints struct {
	CPU    *RequestResourceConstraint `json:"cpu,omitempty"`
	Memory *RequestResourceConstraint `json:"memory,omitempty"`
}
