#!/usr/bin/env bash

set -e

kind_worker_name=$1

source "$(dirname "${BASH_SOURCE}")/util.sh"

util::stop-container "$kind_worker_name"
kubectl wait --for=condition=Ready=Unknown "node/${kind_worker_name}" --timeout=1m
