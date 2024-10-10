### `KubeTurbo Release Notes`

##### 6.3 Image
This image includes new features to help with node consolidation.
1. User can specify how to identify master nodes, which will not suspend. Identify either by node name, or by key value pair of node labels and values.  If no match is found, then these values are ignored.  Consult the deploy for more info.
2. User can specify which pods are to be treated as a Daemon Set, which means that these pods are not counted as workload to keep a node running, i.e. if these are the only pods left on a node, the node can suspend.  Default behavior is any controller kind = daemonSet will automatically have this behavior.  If any pods are specified using the configMap, then the default is overwritten. Identify pods by providing optional configurations of all pods running in a namespace(s) or pod naming convention.

Fixes:

##### 6.2 image
