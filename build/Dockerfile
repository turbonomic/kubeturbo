FROM registry.access.redhat.com/ubi9-micro
LABEL maintainer="Turbonomic <turbodeploy@turbonomic.com>"
ARG TARGETPLATFORM
ARG BUILDPLATFORM

RUN echo "Running on $BUILDPLATFORM, and building for $TARGETPLATFORM"

### Atomic/OpenShift Labels - https://github.com/projectatomic/ContainerApplicationGenericLabels
LABEL name="Hybrid Cloud Container" \
    vendor="Turbonomic" \
    version="v8.0.0" \
    release="1" \
    summary="Performance assurance for the applications in Openshift" \
    description="Hybrid Cloud Container leverages Turbonomic control platform, to assure the performance of micro-services running in OpenShift, as well as the efficiency of underlying infrastructure." \
    ### Required labels above - recommended below
    url="https://www.turbonomic.com" \
    io.k8s.description="Hybrid Cloud Container will monitor and control the entire stack, from OpenShift down to your underlying infrastructure. " \
    io.k8s.display-name="Hybrid Cloud Container" \
    io.openshift.expose-services="" \
    io.openshift.tags="turbonomic,Hybrid Cloud Container"

### Atomic Help File - Write in Markdown, it will be converted to man format at build time.
### https://github.com/projectatomic/container-best-practices/blob/master/creating/help.adoc
COPY help.md /tmp/

### add licenses to this directory
COPY licenses /licenses

COPY Dockerfile /Dockerfile


### Setup user for build execution and application runtime
ENV APP_ROOT=/opt/turbonomic \
    USER_UID=10001
ENV PATH=$PATH:${APP_ROOT}/bin

RUN mkdir -p ${APP_ROOT}/bin
COPY ${TARGETPLATFORM}/kubeturbo ${APP_ROOT}/bin/kubeturbo
RUN chmod -R ug+x ${APP_ROOT}/bin && sync && \
    chmod -R g=u ${APP_ROOT}

####### Add app-specific needs below. #######
### Containers should NOT run as root as a good practice
USER ${USER_UID}
WORKDIR ${APP_ROOT}
ENTRYPOINT ["/opt/turbonomic/bin/kubeturbo"]
