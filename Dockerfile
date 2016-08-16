# Set the base image
FROM ubuntu

# Set the file maintainer
MAINTAINER Dongyi Yang <dongyi.yang@vmturbo.com>

ADD _output/kubeturbo /bin/kubeturbo

ENTRYPOINT ["/bin/kubeturbo"]



