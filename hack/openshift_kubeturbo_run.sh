#!/usr/bin/env bash

OUTPUT_DIR=${OUTPUT_DIR:-"_output"}

./${OUTPUT_DIR}/kubeturbo \
	 --v=3 \
	 --kubeconfig="$HOME/.kube/config" \
	 --cadvisor-port=9999 \
	 --usevmware=false \
	 --testingflag="./hack/testing-flag.json" \
	 --turboconfig="./hack/container-conf.json" > "/tmp/kubeturbo.log" 2>&1 &
