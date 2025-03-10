#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source "$(dirname "${BASH_SOURCE}")/util.sh"

logEnd() {
  local msg='Download done.'
  [ "$1" -eq 0 ] || msg='Error downloading binaries.'
  echo "$msg"
  rm -rf istio-${istio_version}
  rm -rf openshift-client.tar.gz
}
trap 'logEnd $?' EXIT

echo "About to download some binaries. This might take a while..."

#kind
kind_url="https://github.com/kubernetes-sigs/kind/releases/download/${kind_version}/${kind_bin_name}"
echo "Download kind"
curl -sLo "${kind_path}" "${kind_url}" && chmod +x "${kind_path}"

#kubectl
echo "Download kubectl"
curl -sLo ${kubectl_path} "https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/${OS}/${ARCH}/kubectl" && chmod +x "${kubectl_path}"

#oc
echo "Download oc"
oc_filename="openshift-client-linux-${ARCH}-rhel8-${oc_version}.tar.gz"
if [ "${OS}" = "darwin" ]; then
  oc_filename="openshift-client-mac-${ARCH}-${oc_version}.tar.gz"
fi
curl -sLo openshift-client.tar.gz https://mirror.openshift.com/pub/openshift-v4/${ARCH}/clients/ocp/${oc_version}/${oc_filename}
tar -C ${dest_dir} -zxvf openshift-client.tar.gz oc
chmod +x ${oc_path}

#istioctl
echo "Download istio"
curl -sL https://istio.io/downloadIstio | ISTIO_VERSION=${istio_version} sh -
cp istio-${istio_version}/bin/istioctl ${istioctl_path}
chmod +x ${istioctl_path}

echo -n "#   kind:           "; "${kind_path}" version
echo -n "#   kubectl:        "; "${kubectl_path}" version --client
echo -n "#   oc:             "; "${oc_path}" version --client
echo -n "#   istioctl:       "; "${istioctl_path}" version --short
