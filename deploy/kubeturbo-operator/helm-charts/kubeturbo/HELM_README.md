**Documentation** is being maintained on the Wiki for this project.  Visit the [Helm Chart details here](https://github.com/turbonomic/kubeturbo/wiki/Helm-Deployment-Details).  Visit [Kubeturbo Wiki](https://github.com/turbonomic/kubeturbo/wiki) for the full documentation, examples and guides. 

## Kubeturbo Deploy via Helm Charts

[Helm](https://helm.sh/) is a kubernetes package manager that allows you to more easily manage charts, which are a way to package all resources associated with an application.  Helm provides a way to package, deploy and update using simple commands, and provides a way to customize or update parameters of your resources, without the worry of yaml formatting. For more info see: [Helm: The Kubernetes Package Manager](https://github.com/helm/helm)  

To use this method, you will already have a helm client and tiller server installed, and are familiar with how to use helm and chart repositories. Go to [Helm Docs](https://helm.sh/docs/using_helm/%23quickstart-guide) to get started.

The Helm Chart provided [here](https://github.com/turbonomic/kubeturbo/tree/master/deploy/kubeturbo) will deploy kubeturbo and create the following resources: 
1. Create a Namespace or Project (default is "turbo")
1. Service Account and binding to cluster-admin clusterrole (default is "turbo-user" with "turbo-all-binding" role)
1. ConfigMap for kubeturbo to connect to the Turbonomic server
1. Deploy kubeturbo Pod


There's no place like home... go back to the [Turbonomic Overview](https://github.com/turbonomic/kubeturbo/wiki/Overview).
