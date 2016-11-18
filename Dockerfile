# Set the base image
FROM ubuntu

# Set the file maintainer
MAINTAINER Dongyi Yang <dongyi.yang@vmturbo.com>

RUN apt-get update
RUN apt-get install -y ca-certificates 
ADD _output/kubeturbo /bin/kubeturbo

ENTRYPOINT ["/bin/kubeturbo"]



