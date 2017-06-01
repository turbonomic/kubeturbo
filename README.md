<p align="center">
  <img src="https://cloud.githubusercontent.com/assets/4391815/26681386/05b857c4-46ab-11e7-8c71-15a46d886834.png">
</p>


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

# **KubeTurbo** 

----

1. [Overview](#overview)
2. [Getting Started](#getting-started)
3. [Kubeturbo Use Cases](#use-cases)
4. [Coming Soon](#coming-soon)

----

## Overview 

Kubeturbo leverages [Turbonomic's](https://turbonomic.com/) patented analysis engine to provide visibility and control across the entire stack in order to assure the performance of running micro-services in Kubernetes Pods, as well as the efficiency of underlying infrastructure.

## Getting Started 
### Prerequisites
* Turbonomic 5.9+ installation
* Kubernetes 1.4+
### Kubeturbo Installation
* [Deploy Kubeturbo](https://github.com/turbonomic/kubeturbo/tree/master/deploy)

<img width="1323" alt="screen shot 2017-06-01 at 10 10 21 am" src="https://cloud.githubusercontent.com/assets/4391815/26683726/c5e06e7c-46b2-11e7-92f2-5f555fe88ea2.png">

## Use Cases
* Full-Stack Visibility by leveraging 50+ existed Turbonomic controllers, from on-prem DataCenter to major public cloud providers. No more shadow IT
  * From Load Balancer all the way down to your physical Infrastructure
  * Real-Time resource monitoring across entire DataCenter
  * Real-Time Cost visibility for your public cloud deployment
<img width="1322" alt="screen shot 2017-06-01 at 10 10 48 am" src="https://cloud.githubusercontent.com/assets/4391815/26683739/cfde3c7e-46b2-11e7-84b5-a39c12f47022.png">
<img width="1320" alt="screen shot 2017-06-01 at 10 11 54 am" src="https://cloud.githubusercontent.com/assets/4391815/26683749/d7813a30-46b2-11e7-9283-0dbc63769d90.png">
<img width="1403" alt="screen shot 2017-06-01 at 10 11 03 am" src="https://cloud.githubusercontent.com/assets/4391815/26683745/d3c42a38-46b2-11e7-9e6c-3587a1480afc.png">

* Provide Rescheduler capability by leveraging The Turbonomic analysis engine **(Execution of moving pods requires Kubeturbo to be the scheduler of the pod)**
  * Consolidating Pods in real-time to increase node efficiency
  * Reschedule Pod in advance to prevent suffering resource congestion from the underlying node
  *	Reschedule Pod to new node added to the cluster
  *	Reschedule Pods that peak together to different nodes, to avoid performance dropping
<img width="1320" alt="screen shot 2017-06-01 at 10 11 31 am" src="https://cloud.githubusercontent.com/assets/4391815/26683755/dccd6ad6-46b2-11e7-8d9f-452b60e827d5.png">


* Right-Sizing your Pod and your entire IT stack
  *	Combining Turbonomic real-time performance monitoring and analysis engine, Turbonomic is able to provide right-sizing information for each individual pod as well as the entire IT stack.
  * Right-sizing up your Pod limit, if necessary, to avoid OOM
  * Right-sizing down your Pod requested resource, if necessary, to avoid resource overprovisioning or overspending in public cloud deployment.
![screen shot 2017-06-01 at 9 56 55 am](https://cloud.githubusercontent.com/assets/4391815/26683094/b330c350-46b0-11e7-91e5-a1db65a89d50.png)


## Coming Soon
* Cluster Federation Control Plane
  * Complete visibility for your K8s deployments across different underlying infrastructures
  * Create affinity/anti-affinity policies directly from Turbonomic UI
  * Improve cost efficiency by consolidating workload across deployments and identifying the cheapest region and provider to deploy your workload
* What-If Planer 
  * A complete What-If sandbox to help you plan your IT changes in advance
  * Plan for workload change: Add/Remove Containers
  * Plan for infrastructure change: Add/Remove/Replace hardware
  * Plan for Cloud Migration: Expense and Savings
  * Cluster Consolidation for federated clusters


