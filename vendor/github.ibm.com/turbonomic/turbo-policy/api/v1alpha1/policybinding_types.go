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

// PolicyBindingSpec defines the desired state of PolicyBinding
type PolicyBindingSpec struct {
	// The reference to a policy
	PolicyRef PolicyReference `json:"policyRef"`

	// The target objects that the policy is applied to
	// +kubebuilder:validation:MinItems:=1
	Targets []PolicyTargetReference `json:"targets"`
}

// PolicyBindingStatus defines the observed state of PolicyBinding
type PolicyBindingStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PolicyBinding is the Schema for the policybindings API
type PolicyBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PolicyBindingSpec   `json:"spec,omitempty"`
	Status PolicyBindingStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PolicyBindingList contains a list of PolicyBinding
type PolicyBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PolicyBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PolicyBinding{}, &PolicyBindingList{})
}
