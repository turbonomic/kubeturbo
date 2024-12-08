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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GitOpsSpec defines the desired state of GitOps configuration
type GitOpsSpec struct {
	// Overrides the default GitOps configuration with custom configuration for the specified app(s).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems:=1
	Configuration []Configuration `json:"config"`
}

// CommitMode describes how Turbonomic events will be processed.
// +kubebuilder:validation:Enum=direct;request
type CommitMode string

const (
	// Actions will produce commit directly within the underlying repository without creating a pull/merge request
	DirectCommit CommitMode = "direct"

	// Actions will result in a pull/merge request being creating within the underlying repository
	RequestCommit CommitMode = "request"
)

type Configuration struct {
	// Specifies the GitOps commit mode.
	// Valid values are:
	// - "direct": actions will produce commit directly within the underlying repository without creating a pull/merge request;
	// - "request": actions will result in a pull/merge request being creating within the underlying repository
	CommitMode CommitMode `json:"commitMode"`
	// Specifies the credentials for the underlying repository (CURRENTLY UNSUPPORTED)
	// +optional
	Credentials Credentials `json:"credentials,omitempty"`
	// A regular expression against which applications will be checked. Application names that match the supplied expression
	// will use the configuration supplied here.
	// NOTE: the selector property is prioritzed over the whitelist.
	Selector string `json:"selector,omitempty"`
	// A whitelist list of application names to which the configuration should apply.
	// NOTE: the selector property is prioritzed over the whitelist.
	// +kubebuilder:validation:MinItems:=1
	Whitelist []string `json:"whitelist,omitempty"`
}

type Credentials struct {
	// Specifies the email address of the user from which commits/PRs will be created
	// +kubebuilder:validation:Format=email
	Email string `json:"email"`
	// Specifies the name of the secret containing credentials for the repository
	SecretName string `json:"secretName"`
	// Specifies the namespace in which the secret containing the credentials exists
	SecretNamespace string `json:"secretNamespace"`
	// Specifies the username from which commits/PRs will be created by
	Username string `json:"username"`
}

// GitOpsStatus defines the observed state of GitOps
type GitOpsStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GitOps is the Schema for the gitops API
type GitOps struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitOpsSpec   `json:"spec,omitempty"`
	Status GitOpsStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GitOpsList contains a list of GitOps
type GitOpsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitOps `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitOps{}, &GitOpsList{})
}
