#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source "$(dirname "${BASH_SOURCE}")/util.sh"

function check-cluster-ready() {
    util::wait-for-condition 'ok' "${oc_path} get --raw=/healthz &> /dev/null" 120
}

echo "Log in the OpenShift cluster"
${oc_path} login --server=${OCP_SERVER_ADDR} --username=${OCP_USERNAME} --password=${OCP_PASSWORD} --insecure-skip-tls-verify=true
check-cluster-ready
${oc_path} config rename-context $(${oc_path} config current-context) rosa
