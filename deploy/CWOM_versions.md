## CWOM Version Mapping to Turbonomic Version

When deploying kubeturbo, since it is a remote probe, you need to provide the server version number of the CWOM Server it is connecting to in the config map resource.
The actual value provided will be the corresponding Turbonomic Server version.

The table below provides a conversion from CWOM to Turbo:


CWOM Version|Turbonomic Version
------------ | -------------
1.0|5.8.3.1
1.1|5.9.1
1.1.3|5.9.3
1.2.0|6.0.3
1.2.1|6.0.6
1.2.2|6.0.9
1.2.3|6.0.11.1
2.0.0|6.1.1
2.0.1|6.1.6
2.0.2|6.1.8
2.0.3|6.1.12
2.1.0|6.2.2
2.1.1|6.2.7.1
2.1.2|6.2.10

The recommended CWOM version to be on is 2.1.2+

There's no place like home... go back to the [Turbonomic Overview](https://github.com/turbonomic/kubeturbo/tree/master/README.md) or the [Deploy Overview](https://github.com/turbonomic/kubeturbo/tree/master/deploy/README.md).