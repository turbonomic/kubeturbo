## Version Mapping between CWOM,Turbonomic and Kubeturbo Versions

When deploying kubeturbo, since it is a remote probe, you need to provide the server version number of the CWOM or Turbo Server kubeturbo will connect to in the config map resource.
The actual value provided will be the corresponding Turbonomic Server version, and a server version you are running will have a preferred kubeturbo image tag.

_Note: versions numbers do **not** have to exactly line up_

Refer to the documentation on the kubeturbo project wiki: [Server Version Mappings for CWOM and Kubeturbo Images]( https://github.com/turbonomic/kubeturbo/wiki/Server-Versions-and-Kubeturbo-Tag-Mappings)
