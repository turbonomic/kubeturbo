<p align="center">
  <img src="https://cloud.githubusercontent.com/assets/4391815/26681386/05b857c4-46ab-11e7-8c71-15a46d886834.png">
</p>


<!--
http://www.apache.org/licenses/LICENSE-2.0.txt


Copyright 2021 Turbonomic

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

![Docker Pulls](https://img.shields.io/docker/pulls/turbonomic/kubeturbo.svg?maxAge=604800)


Documentation is being maintained on the Wiki for this project.  Visit [Kubeturbo Wiki](https://github.com/turbonomic/kubeturbo/wiki) for the full documentation, examples and guides.

## Overview 

Kubeturbo leverages [Turbonomic's](https://turbonomic.com/) patented analysis engine to provide observability WITH control across the entire stack in order to assure the performance of running micro-services on Kubernetes platforms, as well as driving efficiency of underlying infrastructure.  You work hard.  Let software make automated resources decisions so you can focus on on-boarding more performant applications.

Use cases and More:
1. Full Stack Management
2. Intelligent SLO Scaling
3. Proactive Rescheduling
4. What's New
5. Supported Platforms


## Full Stack Management
Starts with Full-Stack Visibility by leveraging 50+ existing Turbonomic controllers, from on-prem DataCenter to major public cloud providers. No more shadow IT
  * From the Business Application all the way down to your physical Infrastructure
  * Continuous Real-Time resource management across entire DataCenter
  * Cost optimization for your public cloud deployment
<img width="1322" src="https://raw.githubusercontent.com/evat-pm/images/aa796457175c77b3954a584c1bd68bd2685eacee/fullStack-containerizedApps.png">

## Intelligent SLO Scaling
Manage the Trade-offs of Performance and Efficiency with Intelligent Vertical and Horizontal scaling that understands the entire IT stack
  *	Combining Turbonomic real-time performance monitoring and analysis engine, Turbonomic is able to provide right-sizing and scaling decisions for each service as well as the entire IT stack.
  *	Scale services based on SLO and simultaneously managed cluster resources to mitigate pending pods
  * Right-sizing up your Pod limit to avoid OOM and address CPU Throttling
  * Right-sizing down your Pod requested resource to avoid resource over-provisioning or overspending in public cloud deployment.
  * Intelligently scale your nodes based on usage, requests, not just pod pending conditions
<img width="1320" src="https://raw.githubusercontent.com/evat-pm/images/aa796457175c77b3954a584c1bd68bd2685eacee/sloScaling-actions.png">
<img width="1320" src="https://raw.githubusercontent.com/evat-pm/images/aa796457175c77b3954a584c1bd68bd2685eacee/vertical-actions.png">

## Proactive Rescheduling
Intelligently, continuously redistribute workload under changing conditions by leveraging The Turbonomic analysis engine 
  * Consolidate Pods in real-time to increase node efficiency
  * Reschedule Pod to prevent performance degradation due to resource congestion from the underlying node
  *	Redistribute Pods to leverage resources when new node capacity comes on line
  *	Reschedule Pods that peak together to different nodes, to avoid performance issues due to "noisy neighbors"
<img width="1320" src="https://raw.githubusercontent.com/evat-pm/images/aa796457175c77b3954a584c1bd68bd2685eacee/moves-nodeOptimize.png">

## What's New
With the release of 8.3.1, we are pleased to announce
  * **CPU Throttling** Turbonomic can now recommend increasing vCPU limit capacity to address slow response times associated with
CPU throttling. As throttling drops and performance improves, it analyzes throttling data holistically to ensure that a
subsequent action to decrease capacity will not result in throttling.
  * **Power10 Support** KubeTurbo now supports Kubernetes clusters that run on Linux ppc64le
(including Power10) architectures. Select the architecture you want from the public Docker Hub repo starting with KubeTurbo image 8.3.1, at
[turbonomic/kubeturbo:8.3.1](https://hub.docker.com/layers/turbonomic/kubeturbo/8.3.1/images/sha256-f1770480f31b974488e25a5c3c2c4633b480dabe28fde5640fd23aebdb54b91e?context=explore).
To deploy Kubeturbo via Operator, use the Operator image at [turbonomic/kubeturbo-operator:8.3](https://hub.docker.com/layers/turbonomic/kubeturbo-operator/8.3/images/sha256-084e096b2321b3c872c3c70df749638cbb9b4f5c3fe5c1461e322cade762d06a?context=explore). Note that
KubeTurbo deployed via the [OpenShift Operator Hub](https://operatorhub.io/operator/kubeturbo) currently only supports x86.

## [Supported Platforms](https://www.turbonomic.com/platform/integrations/?_integrations_filter_buttons=container-platforms)
Any upstream compliant Kubernetes distribution, starting with v1.8+ to current GA
<img width="1320" src="https://raw.githubusercontent.com/evat-pm/images/aa796457175c77b3954a584c1bd68bd2685eacee/kubeturbo-support.png">
