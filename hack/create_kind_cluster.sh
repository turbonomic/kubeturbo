#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source "$(dirname "${BASH_SOURCE}")/util.sh"
KIND_IMAGE="${KIND_IMAGE:-}"
KIND_TAG="${KIND_TAG:-}"
OS=`uname`

function check-cluster-ready() {
    util::wait-for-condition 'ok' "${kubectl_path} --context kind-kind get --raw=/healthz &> /dev/null" 120
}

function create-cluster() {
  cat <<EOF | ${kind_path} create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
kubeadmConfigPatches:
- |
  apiVersion: kubelet.config.k8s.io/v1beta1
  kind: KubeletConfiguration
  "evictionHard": {
    "memory.available": "20%",
    "nodefs.available": "20%",
    "imagefs.available": "20%",
  }
nodes:
- role: control-plane
- role: worker
- role: worker
- role: worker
EOF

  echo "Waiting for cluster to be ready"
  check-cluster-ready
}


echo "Creating kind cluster"
create-cluster
${kubectl_path} config use-context kind-kind


docker cp ./test/config/static-web.yaml kind-worker2:/etc/kubernetes/manifests/
docker cp ./test/config/static-web.yaml kind-worker3:/etc/kubernetes/manifests/

echo "Cluster creation complete."
