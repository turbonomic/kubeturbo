FROM registry.access.redhat.com/rhel7
MAINTAINER Enlin Xu <enlin.xu@turbonomic.com>
ARG GIT_COMMIT
ENV GIT_COMMIT ${GIT_COMMIT}

### Atomic/OpenShift Labels - https://github.com/projectatomic/ContainerApplicationGenericLabels
LABEL name="Hybrid Cloud Container" \
      vendor="Turbonomic" \
      version="6.1" \
      release="1" \
      summary="Performance assurance for the applications in Openshift" \
      description="Hybrid Cloud Container leverages Turbonomic control platform, to assure the performance of micro-services running in OpenShift, as well as the efficiency of underlying infrastructure." \
### Required labels above - recommended below
      url="http://www.turbonomic.com" \
      run='docker run -tdi --name ${NAME} vmturbo/kubeturbo:latest' \
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

### Add necessary Red Hat repos here
RUN REPOLIST=rhel-7-server-rpms,rhel-7-server-optional-rpms \
### Add your package needs here
    INSTALL_PKGS="golang-github-cpuguy83-go-md2man ca-certificates" && \
    yum -y update-minimal --disablerepo "*" --enablerepo rhel-7-server-rpms --setopt=tsflags=nodocs \
      --security --sec-severity=Important --sec-severity=Critical && \
    yum -y install --disablerepo "*" --enablerepo ${REPOLIST} --setopt=tsflags=nodocs ${INSTALL_PKGS} && \
### help file markdown to man conversion
    go-md2man -in /tmp/help.md -out /help.1 && \
    yum clean all

### Setup user for build execution and application runtime
ENV APP_ROOT=/opt/turbonomic \
    USER_NAME=default \
    USER_UID=10001
ENV PATH=$PATH:${APP_ROOT}/bin

RUN mkdir -p ${APP_ROOT}/bin
#COPY kubeturbo.sh ${APP_ROOT}/bin/kubeturbo
COPY kubeturbo ${APP_ROOT}/bin/kubeturbo
RUN chmod -R ug+x ${APP_ROOT}/bin && sync && \
    useradd -l -u ${USER_UID} -r -g 0 -d ${APP_ROOT} -s /sbin/nologin -c "${USER_NAME} user" ${USER_NAME} && \
    chown -R ${USER_UID}:0 ${APP_ROOT} && \
    chmod -R g=u ${APP_ROOT}

####### Add app-specific needs below. #######
### Containers should NOT run as root as a good practice
USER 10001
WORKDIR ${APP_ROOT}
ENTRYPOINT ["/opt/turbonomic/bin/kubeturbo"]
