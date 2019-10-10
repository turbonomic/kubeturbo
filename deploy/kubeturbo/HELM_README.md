## Kubeturbo Deploy via Helm Charts

You can deploy kubeturbo with a Helm Chart. The Helm Chart provided [here](https://github.com/turbonomic/kubeturbo/tree/master/deploy/kubeturbo) will create the following resources: 
1. Create a Namespace or Project (default is "turbo")
1. Service Account and binding to cluster-admin clusterrole (default is "turbo-user" with "turbo-all-binding" role)
1. ConfigMap for kubeturbo to connect to the Turbonomic server
1. Deploy kubeturbo Pod

Documents are maintained on the wiki for this project.  Go to the [Turbonomic Wiki Home](https://github.com/turbonomic/kubeturbo/wiki) or start with the [Turbonomic Overview](https://github.com/turbonomic/kubeturbo/wiki/Overview)
