**Documentation** is being maintained on the Wiki for this project.  Visit the [Kubeturbo Operator page here](https://github.com/turbonomic/kubeturbo/wiki/Operator-Details).  Visit [Kubeturbo Wiki](https://github.com/turbonomic/kubeturbo/wiki) for the full documentation, examples and guides. 

## Kubeturbo Deploy via Operator

An [Operator](https://coreos.com/operators/) is an extension of kubernetes to manage an [application's lifecycle using a custom resource](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) 

To use this method, you will deploy an operator and configure a custom resource (via a cr yaml file) to deploy kubeturbo.  These steps can also be performed using the OCP Operator Framework.

The Operator resource definitions provided [here](https://github.com/turbonomic/kubeturbo/tree/master/deploy/kubeturbo-operator) will deploy resources via helm chart for the operator and kubeturbo.  The steps to deploy are:
1. Create Operator service account, role and role-binding
1. Deploy Operator
1. Update the Custom Resource manifest (cr.yaml)
1. Deploy Kubeturbo


There's no place like home... go back to the [Turbonomic Overview](https://github.com/turbonomic/kubeturbo/wiki/Overview).
