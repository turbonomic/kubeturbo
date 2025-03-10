#!/bin/bash

# Create the resources for SCC impersination
NAMESPACE=$1

if [ -z $NAMESPACE ]; then
  echo "Usage: $0 <namespace>" && exit 1
fi

COMMAND=oc

read -r -d '' CLUSTER_ROLE_BINDING << EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubeturbo-scc-pod-crud-${NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kubeturbo-scc-pod-crud-${NAMESPACE}
subjects:
EOF

SCC_LEVELS=$($COMMAND get scc --no-headers -o custom-columns=":metadata.name")

for SCC_LEVEL in $SCC_LEVELS; do
    $COMMAND create serviceaccount kubeturbo-scc-${SCC_LEVEL} -n ${NAMESPACE}

cat << EOF | $COMMAND create -n ${NAMESPACE} -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kubeturbo-scc-use-${SCC_LEVEL}
  namespace: ${NAMESPACE}
rules:
- apiGroups:
  - security.openshift.io
  resourceNames:
  - ${SCC_LEVEL}
  resources:
  - securitycontextconstraints
  verbs:
  - use
EOF

cat << EOF | $COMMAND create -n ${NAMESPACE} -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kubeturbo-scc-use-${SCC_LEVEL}
  namespace: ${NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kubeturbo-scc-use-${SCC_LEVEL}
subjects:
- kind: ServiceAccount
  name: kubeturbo-scc-${SCC_LEVEL}
  namespace: ${NAMESPACE}
EOF

read -r -d '' CLUSTER_ROLE_BINDING << EOF
${CLUSTER_ROLE_BINDING}
- kind: ServiceAccount
  name: kubeturbo-scc-${SCC_LEVEL}
  namespace: ${NAMESPACE}
EOF
done

cat << EOF | $COMMAND create -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubeturbo-scc-pod-crud-${NAMESPACE}
rules:
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - '*'
EOF

cat << EOF | $COMMAND create -f -
${CLUSTER_ROLE_BINDING}
EOF
