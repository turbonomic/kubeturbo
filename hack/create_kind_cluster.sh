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
  local image_arg=""
  if [[ "${KIND_IMAGE}" ]]; then
    image_arg="--image=${KIND_IMAGE}"
  elif [[ "${KIND_TAG}" ]]; then
    image_arg="--image=kindest/node:${KIND_TAG}"
  fi

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
  image: kindest/node:v1.18.0@sha256:0e20578828edd939d25eb98496a685c76c98d54084932f76069f886ec315d694
- role: worker
  image: kindest/node:v1.18.0@sha256:0e20578828edd939d25eb98496a685c76c98d54084932f76069f886ec315d694
- role: worker
  image: kindest/node:v1.18.0@sha256:0e20578828edd939d25eb98496a685c76c98d54084932f76069f886ec315d694
- role: worker
  image: kindest/node:v1.18.0@sha256:0e20578828edd939d25eb98496a685c76c98d54084932f76069f886ec315d694 
EOF

  echo "Waiting for cluster to be ready"
  check-cluster-ready
}


echo "Creating kind cluster"
create-cluster
${kubectl_path} config use-context kind-kind
${kubectl_path} label node kind-worker foo=bar

echo "Cluster creation complete."
