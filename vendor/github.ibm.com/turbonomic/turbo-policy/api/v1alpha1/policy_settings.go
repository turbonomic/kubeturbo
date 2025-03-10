/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	ResponseTime      = "ResponseTime"
	ServiceTime       = "ServiceTime"
	QueuingTime       = "QueuingTime"
	ConcurrentQueries = "ConcurrentQueries"
	Transaction       = "Transaction"
	LLMCache          = "LLMCache"
)

type SLOHorizontalScalePolicySetting struct {
	// The name of the policy setting
	Name string `json:"name"`

	// The value of the policy setting
	Value v1.JSON `json:"value"`
}

type SamplePeriod struct {
	// +kubebuilder:validation:Enum="none";"1d";"3d";"7d"
	Min *MinObservationPeriod `json:"min,omitempty"`
	// +kubebuilder:validation:Enum="90d";"30d";"7d"
	Max *MaxObservationPeriod `json:"max,omitempty"`
}

// Resize increment constants for CPU and Memory
type ResizeIncrements struct {
	CPU    *resource.Quantity `json:"cpu,omitempty"`
	Memory *resource.Quantity `json:"memory,omitempty"`
}

// ContainerVerticalScaleSpec defines the desired state of ContainerVerticalScale
type ContainerVerticalScalePolicySettings struct {
	// +kubebuilder:default:={cpu:{max:64, min:"500m", recommendAboveMax:true, recommendBelowMin:false}, memory:{max:"104857M", min:"10M", recommendAboveMax:true, recommendBelowMin:true}}
	// +optional
	Limits *LimitResourceConstraints `json:"limits,omitempty"`

	// +kubebuilder:default:={cpu:{min:"10m", recommendBelowMin:false}, memory:{min:"10M", recommendBelowMin:true}}
	// +optional
	Requests *RequestResourceConstraints `json:"requests,omitempty"`

	// +kubebuilder:default:={cpu:"100m", memory:"128M"}
	// +optional
	Increments *ResizeIncrements `json:"increments,omitempty"`

	// +kubebuilder:default:={min:"1d", max:"30d"}
	// +optional
	ObservationPeriod *SamplePeriod `json:"observationPeriod,omitempty"`

	// +kubebuilder:default:="high"
	// +kubebuilder:validation:Enum="low";"medium";"high"
	// +optional
	RateOfResize *ResizeRate `json:"rateOfResize,omitempty"`

	// +kubebuilder:default:="p99"
	// +kubebuilder:validation:Enum="p90";"p95";"p99";"p99_1";"p99_5";"p99_9";"p100"
	// +optional
	Aggressiveness *PercentileAggressiveness `json:"aggressiveness,omitempty"`

	// CpuThrottlingTolerance defines the acceptable level of throttling and directly impacts the resize actions generated on CPU Limits.
	// The value ranges from 0% to 70%
	// +kubebuilder:validation:Pattern:=`^(?:[0-6]?[0-9]|70|\b0)%$`
	// +kubebuilder:default:="20%"
	// +optional
	CpuThrottlingTolerance *string `json:"cpuThrottlingTolerance,omitempty"`
}
