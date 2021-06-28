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

![Docker Pulls](https://img.shields.io/docker/pulls/turbonomic/kubeturbo.svg?maxAge=604800)


Documentation is being maintained on the Wiki for this project.  Visit [Kubeturbo Wiki](https://github.com/turbonomic/kubeturbo/wiki) for the full documentation, examples and guides.


## Overview 

Kubeturbo leverages [Turbonomic's](https://turbonomic.com/) patented analysis engine to provide visibility and control across the entire stack in order to assure the performance of running micro-services in Kubernetes Pods, as well as the efficiency of underlying infrastructure.


## Use Cases
* Full-Stack Visibility by leveraging 50+ existing Turbonomic controllers, from on-prem DataCenter to major public cloud providers. No more shadow IT
  * From Load Balancer all the way down to your physical Infrastructure
  * Real-Time resource monitoring across entire DataCenter
  * Real-Time Cost visibility for your public cloud deployment

<img width="1322" alt="screen shot 2017-06-01 at 10 10 48 am" src="https://cloud.githubusercontent.com/assets/4391815/26683739/cfde3c7e-46b2-11e7-84b5-a39c12f47022.png">
<img width="1320" alt="screen shot 2017-06-01 at 10 11 54 am" src="https://cloud.githubusercontent.com/assets/4391815/26683749/d7813a30-46b2-11e7-9283-0dbc63769d90.png">
<img width="1403" alt="screen shot 2017-06-01 at 10 11 03 am" src="https://cloud.githubusercontent.com/assets/4391815/26683745/d3c42a38-46b2-11e7-9e6c-3587a1480afc.png">

* Intelligently, continuously redistribute workload under changing conditions by leveraging The Turbonomic analysis engine 
  * Consolidate Pods in real-time to increase node efficiency
  * Reschedule Pod to prevent performance degradation due to resource congestion from the underlying node
  *	Reschedule Pod to leverage resources from new node added to the cluster
  *	Reschedule Pods that peak together to different nodes, to avoid performance issues due to "noisy neighbors"
<img width="1320" alt="screen shot 2017-06-01 at 10 11 31 am" src="https://cloud.githubusercontent.com/assets/4391815/26683755/dccd6ad6-46b2-11e7-8d9f-452b60e827d5.png">


* Right-Sizing your Pod and your entire IT stack
  *	Combining Turbonomic real-time performance monitoring and analysis engine, Turbonomic is able to provide right-sizing information for each service as well as the entire IT stack.
  * Right-sizing up your Pod limit, if necessary, to avoid OOM
  * Right-sizing down your Pod requested resource, if necessary, to avoid resource over-provisioning or overspending in public cloud deployment.
![screen shot 2017-06-01 at 9 56 55 am](https://cloud.githubusercontent.com/assets/4391815/26683094/b330c350-46b0-11e7-91e5-a1db65a89d50.png)




