## Version Mapping between CWOM,Turbonomic and Kubeturbo Versions

When deploying kubeturbo, since it is a remote probe, you need to provide the server version number of the CWOM or Turbo Server kubeturbo will connect to in the config map resource.
The actual value provided will be the corresponding Turbonomic Server version.  The server versions you are running will have a preferred kubeturbo image tag.

_Note: versions numbers do **not** have to exactly line up_

The table below provides a conversion from CWOM to Turbo with preferred kubeturbo image tag:


CWOM Version|Turbonomic Version|kubeturbo DockerHub|kubeturbo RHCC
------------ | ------------- | ------------- | -------------
 NA|6.3.1 - 6.3.3|6.3|6.3
2.1.2|6.2.10|6.2|6.2.2
2.1.1|6.2.7.1|6.2|6.2.2
2.1.0|6.2.2|6.2|6.2.2
2.0.3|6.1.12|6.2|6.2.2
2.0.2|6.1.8|6.2|6.2.2
2.0.1|6.1.6|6.2|6.2.2
2.0.0|6.1.1|6.2|6.2.2
1.2.3|6.0.11.1|(upgrade)
1.2.2|6.0.9|(upgrade)
1.2.1|6.0.6|(upgrade)
1.2.0|6.0.3|(upgrade)
1.1.3|5.9.3|(upgrade)
1.1|5.9.1|(upgrade)
1.0|5.8.3.1|(upgrade)

The recommended CWOM version to be on is 2.1.2+

Kubeturbo images can be on [DockerHub](https://hub.docker.com/r/vmturbo/kubeturbo/) or the [Red Hat Container Catalog](https://access.redhat.com/containers/#/product/aa909a40e026139e)

There's no place like home... go back to the [Turbonomic Overview](https://github.com/turbonomic/kubeturbo/tree/master/README.md) or the [Deploy Overview](https://github.com/turbonomic/kubeturbo/tree/master/deploy/README.md).