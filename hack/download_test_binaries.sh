#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source "$(dirname "${BASH_SOURCE}")/util.sh"

logEnd() {
  local msg='Download done.'
  [ "$1" -eq 0 ] || msg='Error downloading binaries.'
  echo "$msg"
}
trap 'logEnd $?' EXIT

echo "About to download some binaries. This might take a while..."

#kind
kind_url="https://github.com/kubernetes-sigs/kind/releases/download/${kind_version}/${kind_bin_name}"
curl -Lo "${kind_path}" "${kind_url}" && chmod +x "${kind_path}"

#kubectl
curl -Lo ${kubectl_path} "https://storage.googleapis.com/kubernetes-release/release/`curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt`/bin/${OS}/${ARCH}/kubectl" && chmod +x "${kubectl_path}"

#oc
curl -Lo openshift-client-linux.tar.gz https://mirror.openshift.com/pub/openshift-v4/${ARCH}/clients/ocp/latest/openshift-client-linux.tar.gz
tar -C ${dest_dir}  -zxvf openshift-client-linux.tar.gz oc
chmod +x ${oc_path}

#istioctl
curl -L https://istio.io/downloadIstio | ISTIO_VERSION=${istio_version} TARGET_ARCH=x86_64 sh -
cp istio-${istio_version}/bin/istioctl ${istioctl_path}
chmod +x ${istioctl_path}

echo -n "#   kind:           "; "${kind_path}" version
echo -n "#   kubectl:        "; "${kubectl_path}" version --client
echo -n "#   oc:             "; "${oc_path}" version --client
echo -n "#   istioctl:       "; "${istioctl_path}" version --short


