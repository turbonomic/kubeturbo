FROM quay.io/operator-framework/helm-operator:v1.36
MAINTAINER Turbonomic <turbodeploy@turbonomic.com>
ARG VERSION

RUN echo "Building kubeturbo-operator:$VERSION"

# Required OpenShift Labels
LABEL name="Kubeturbo Operator" \
      vendor="IBM" \
      version=$VERSION \
      release=$VERSION \
      summary="This is the kubeturbo operator." \
      description="This operator will deploy an instance of kubeturbo." \
### Required labels above - recommended below
      url="https://www.ibm.com/products/turbonomic" \
      io.k8s.description="Turbonomic Workload Automation Platform simultaneously optimizes performance, compliance, and cost in real-time. Workloads are precisely resourced, automatically, to perform while satisfying business constraints.  " \
      io.k8s.display-name="Kubeturbo Operator" \
      io.openshift.expose-services="" \
      io.openshift.tags="turbonomic, Multicloud Container"

USER root
# Update security library
RUN microdnf update -y krb5-libs
# Required Licenses
COPY licenses /licenses
# Copy helm charts
COPY watches.yaml ${HOME}/watches.yaml
COPY helm-charts/ ${HOME}/helm-charts/
# Set default version number
RUN sed -i "s/VERSION/$VERSION/g" ${HOME}/helm-charts/kubeturbo/values.yaml
# Change user
USER ${USER_UID}
