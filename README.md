<!--
http://www.apache.org/licenses/LICENSE-2.0.txt


Copyright 2015 Turbonomic

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# **KubeTurbo** <sup><sub>_Autonomic Scheduling for Kubernetes Implementations with Turbonomic_</sub></sup>

----

1. [Overview](#overview)
2. [Getting Started](#getting-started)
  * [System Requirements](#system-requirements)

----

## Overview 

Turbonomic will discover application containers 
running across all pods in a Kubernetes cluster. In order to add this target, you must create a Kubeturbo configuration file which includes your Turbonomic credentials,and a custom pod definition that will be used by Kubelet to create a mirror pod running the Kubeturbo service.

## Getting Started 
### System Requirements 

* Turbonomic 5.9+ installation
* Kubernetes 1.4+

## Creating the Configuration File:

