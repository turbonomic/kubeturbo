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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SLOHorizontalScaleSpec defines the desired state of SLOHorizontalScale
type SLOHorizontalScaleSpec struct {
	// The minimum number of replicas of a service
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10000
	// +kubebuilder:default:=1
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// The maximum number of replicas of a service
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10000
	// +kubebuilder:default:=10000
	// +optional
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// The objectives of this SLOHorizontalScale policy
	// +kubebuilder:default:={{name:ResponseTime,value:2000},{name:Transaction,value:10}}
	// +kubebuilder:validation:MinItems:=1
	Objectives []PolicySetting `json:"objectives,omitempty"`

	// The behavior of SLO driven horizontal scale actions
	// +kubebuilder:default:={scaleUp:Manual,scaleDown:Manual}
	// +optional
	Behavior ActionBehavior `json:"behavior,omitempty"`
}

// SLOHorizontalScaleStatus defines the observed state of SLOHorizontalScale
type SLOHorizontalScaleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=sloscale

// SLOHorizontalScale is the Schema for the slohorizontalscales API
type SLOHorizontalScale struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SLOHorizontalScaleSpec   `json:"spec,omitempty"`
	Status SLOHorizontalScaleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SLOHorizontalScaleList contains a list of SLOHorizontalScale
type SLOHorizontalScaleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SLOHorizontalScale `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SLOHorizontalScale{}, &SLOHorizontalScaleList{})
}
