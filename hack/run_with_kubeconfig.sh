#!/usr/bin/env bash

OUTPUT_DIR=${OUTPUT_DIR:-"_output"}

./${OUTPUT_DIR}/kubeturbo \
	 --v=3 \
	 --kubeconfig="$HOME/.kube/config" \
	 --testingflag="./hack/testing-flag.json" \
	 --turboconfig="./hack/minikube-conf.json" > "/tmp/kubeturbo.log" 2>&1 &
