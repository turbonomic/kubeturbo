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

  ${kind_path} create cluster

  echo "Waiting for cluster to be ready"
  check-cluster-ready
}


echo "Creating kind cluster"
create-cluster
kubectl config use-context kind-kind

echo "Cluster creation complete."
