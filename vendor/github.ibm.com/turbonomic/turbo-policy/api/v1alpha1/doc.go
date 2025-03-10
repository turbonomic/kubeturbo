// Copyright 2023 Turbonomic
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package turbo-policy contains Kubernetes CustomResourceDefinitions for generating Turbonomic policies. These CustomResources allow
application owners to declarativly configure policies in a Kubernetes cluster moniroted bu Turbo to allow for easy configuration
of Turbonomic policies. This permits application owners accustomed to working with Kubernetes resources to define Turbo policies
as Kubernetes resources, as opposed to using the UI (to which they may not have access).

# SLO Horizontal Scaling Policies

For more information SLO Horizontal Scaling Policies see the [official documentation/https://www.ibm.com/docs/en/tarm/latest?topic=service-kubernetes-policies#policy_defaults_Service_K8s__CRK8s__title__1]

# Vertical Scaling Policies

For more infomation about Vertical Scaling Policies, see the [official documentation/https://www.ibm.com/docs/en/tarm/latest?topic=spec-container-policies]

# Examples

Sample policy and policy binding CustomResources can be found [here/https://github.com/turbonomic/turbo-policy/tree/main/config/samples]
*/
package v1alpha1
