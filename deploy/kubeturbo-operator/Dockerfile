FROM quay.io/operator-framework/helm-operator:v1.17

# Required OpenShift Labels
LABEL name="Kubeturbo Operator" \
      vendor="Turbonomic" \
      version="8" \
      release="4" \
      summary="This is the kubeturbo operator." \
      description="This operator will deploy an instance of kubeturbo." \
### Required labels above - recommended below
      url="https://www.turbonomic.com" \
      run='docker run -tdi --name ${NAME} turbonomic/kubeturbo-operator:8.4' \
      io.k8s.description="Turbonomic Workload Automation Platform simultaneously optimizes performance, compliance, and cost in real-time. Workloads are precisely resourced, automatically, to perform while satisfying business constraints.  " \
      io.k8s.display-name="Kubeturbo Operator" \
      io.openshift.expose-services="" \
      io.openshift.tags="turbonomic, Multicloud Container"

# Required Licenses
COPY licenses /licenses

COPY watches.yaml ${HOME}/watches.yaml
COPY helm-charts/ ${HOME}/helm-charts/
