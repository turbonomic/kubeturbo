## Kubeturbo Deployment and Prerequisites #

The kubeturbo pod can be easily deployed one of 2 ways:
1. [Helm Chart](#helm-chart)
2. [Deploy Resources via yaml](#deploy-with-yamls)

This document describes common prerequisites, and an overview of each method.  Details will be in the directories. 

## Prerequisites

* OpenShift release 3.4 or higher, kubernetes version 1.8 or higher including any k8s upstream compliant distribution
* Turbonomic Server version 5.9 or higher is installed, running, and the following information:
    * Turbonomic Server URL https://<TurboIPaddressOrFQDN>
    * Turbonomic username with administrator role, and password
    * Turbonomic Server Version.  To get this from the UI, go to Settings -> Updates -> About and use the numeric version such as “6.0.11” or “6.2.0” (Build details not required)
    * Looking for version mappings? Running CWOM? Go here to see conversion chart for [CWOM -> Turbonomic Server -> kubeturbo version](https://github.com/turbonomic/kubeturbo/tree/master/deploy/version_mapping_kubeturbo_Turbo_CWOM.md) 
* Access and Permissions to create all the resources required:
    * User needs **cluster-admin cluster role level access** to be able to create the following resources if required: namespace, service account, and cluster role binding for the service account.
* Repo Access and Network requirements.  Refer to Figure below “Turbonomic and Kubernetes Network Detail”.
    * Instructions assume the node you are deploying to has internet access to pull the kubeturbo image from the DockerHub repository, or your environment is configured with a repo accessible by the node.  Images are available from Dockerhub and RedHat Container Catalog. The exact Tag required will be provided for you by Turbonomic.
        * [Docker hub](https://hub.docker.com/r/vmturbo/kubeturbo/)
        * [RedHat Container Catalog](https://access.redhat.com/containers/#/product/aa909a40e026139e) 
    * Kubeturbo pod will have https/tcp access to the kubelet on every node (if default 10255 is not configured, specify port and https in kubeturbo deploy)
    * Kubeturbo pod will have https/tcp access to the Turbonomic Server
    * Proxies between kubeturbo and Turbonomic Server need to allow websocket communication.
* One kubeturbo pod will be deployed per cluster or per control plane when using stretch clusters. Kubeturbo pod will run with a service account with cluster-admin role
* This pod will typically run with no more than 512 Mg Memory, and maximum volume space of 10 G.

![turboNetwork_anyIAASanyK8S.png](https://github.com/evat-pm/images/blob/master/turboNetwork_anyIAASanyK8S.png)

## Helm Chart

Helm charts are an easy way to deploy and update kubeturbo.  We provide you a helm chart that you can download locally, and install specifying a few parameters.

For more details go to [HELM_README.md](https://github.com/turbonomic/kubeturbo/tree/master/deploy/kubeturbo/HELM_README.md) under kubeturbo/deploy/kubeturbo/


## Deploy with YAMLs

You can deploy the kubeturbo pod using yamls that define the resources required.  Below is an overview.  For more information, go to [YAMLS_README.md](https://github.com/turbonomic/kubeturbo/tree/master/deploy/kubeturbo_yamls/YAMLS_README.md) under kubeturbo/deploy/kubeturbo_yamls/

Strongly advise you to use the sample yamls provided [here](https://github.com/turbonomic/kubeturbo/tree/master/deploy/kubeturbo_yamls).

1. Create a namespace.  Use an existing namespace, or create one where to deploy kubeturbo. The yaml examples will use `turbo`.

2. Create a service account, and add the role of cluster-admin. Assign `cluster-admin` role with a cluster role binding.  Note this role can have view only access, which will allow for metrics to be collected but cannot execute actions.

3. Create a configMap for kubeturbo, The <TURBONOMIC_SERVER_VERSION> is Turbonomic release version, e.g. 6.3.0 or 6.2.8.  To distinguish between different k8s clusters, supply a targetName value which will name the k8s cluster groups created in Turbonomic.

4. Using a deployment type, deploy kubeturbo

5. Validate that you see Containers and Container Pod entities in the Turbonomic Supply Chain, and collecting data.

There's no place like home... go back to the [Turbonomic Overview](https://github.com/turbonomic/kubeturbo/tree/master/README.md).