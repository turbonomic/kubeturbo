# Set the base image
FROM alpine:3.3

# Set the file maintainer
MAINTAINER Dongyi Yang <dongyi.yang@vmturbo.com>

RUN apk --update upgrade && apk add ca-certificates && update-ca-certificates
COPY ./kubeturbo.linux /bin/kubeturbo 

ENTRYPOINT ["/bin/kubeturbo"]



