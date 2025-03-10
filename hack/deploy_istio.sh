#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source "$(dirname "${BASH_SOURCE}")/util.sh"

echo "Start deploying Istio on the cluster."
${istioctl_path} install --set profile=demo -y
echo "Istio deployment complete."
