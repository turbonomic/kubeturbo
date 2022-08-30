#!/usr/bin/env bash

set -e

kind_worker_name=$1
echo $BASH_SOURCE
source "$(dirname "${BASH_SOURCE}")/util.sh"
docker --version
util::stop-container "$kind_worker_name"
${kubectl_path} version
kubectl wait --for=condition=Ready=Unknown "node/${kind_worker_name}" --timeout=1m
