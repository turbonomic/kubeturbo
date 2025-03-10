**Documentation** is being maintained on the Wiki for this project.  Visit the [Deploy Resources via yaml](https://github.com/turbonomic/kubeturbo/wiki/Yaml-Deployment-Details).  Visit [Kubeturbo Wiki](https://github.com/turbonomic/kubeturbo/wiki) for the full documentation, examples and guides. 

## Kubeturbo Deploy via YAMLs


This document describes the resources you will create to deploy kubeturbo, and values you would want to change for your deployment.  It is **strongly advised** you start with the sample yamls provided [here](https://github.com/turbonomic/kubeturbo/tree/master/deploy/kubeturbo_yamls).

#### Resources Overview
**1.** Create a namespace

Use an existing namespace, or create one where to deploy kubeturbo. The yaml examples all will use `turbo`.

**2.** Create a service account, and add the role of cluster-admin

Note: This cluster-admin role can be view only, which will allow for metrics but will not allow for actions to execute.  For examples of a customized admin role narrowed to minimum resources and verbs, see the sample [turbo-admin.yaml](https://github.com/turbonomic/kubeturbo/blob/master/deploy/kubeturbo_yamls/turbo-admin.yaml). For a minimal admin with read only see [turbo-reader.yaml](https://github.com/turbonomic/kubeturbo/blob/master/deploy/kubeturbo_yamls/turbo-reader.yaml) 

**3.** Create a configMap for kubeturbo. Running CWOM? Refer to [Server Versions and Kubeturbo Tag Mappings](https://github.com/turbonomic/kubeturbo/wiki/Server-Versions-and-Kubeturbo-Tag-Mappings) for the mapping of CWOM to Turbo versions. 
The ConfigMap serves two functions, depending on the kubeturbo image being used.
1.	Defines how to connect to the Turbonomic Server. The Turbonomic Server instance details are defined under “communicationConfig”, and optionally what the cluster is identified as in the Turbo UI under “targetConfig”.
2.	How to identify nodes by role and create HA policies. 

**4.** Create a deployment for kubeturbo. 



There's no place like home... go back to the [Turbonomic Overview](https://github.com/turbonomic/kubeturbo/wiki/Overview).
