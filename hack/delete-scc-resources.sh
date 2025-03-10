#!/bin/bash

# Delete the resources for SCC impersination
NAMESPACE=$1

if [ -z $NAMESPACE ]; then
  echo "Usage: $0 <namespace>" && exit 1
fi

COMMAND=oc

for it in $($COMMAND -n ${NAMESPACE} get sa,role,rolebinding -o name | grep "kubeturbo-scc"); do $COMMAND -n ${NAMESPACE} delete ${it}; done

$COMMAND delete ClusterRole kubeturbo-scc-pod-crud-${NAMESPACE}
$COMMAND delete ClusterRoleBinding kubeturbo-scc-pod-crud-${NAMESPACE}