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

type ActionMode string

const (
	Automatic ActionMode = "Automatic"
	Manual    ActionMode = "Manual"
	Recommend ActionMode = "Recommend"
	Disabled  ActionMode = "Disabled"
)

// ActionBehavior defines the action type and its corresponding mode
type ActionBehavior struct {
	// The Action mode of HorizontalScaleUp action
	// +kubebuilder:validation:Enum=Automatic;Manual;Recommend;Disabled
	// +optional
	HorizontalScaleUp *ActionMode `json:"scaleUp,omitempty"`

	// The Action mode of HorizontalScaleDown action
	// +kubebuilder:validation:Enum=Automatic;Manual;Recommend;Disabled
	// +optional
	HorizontalScaleDown *ActionMode `json:"scaleDown,omitempty"`
}
