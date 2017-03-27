#!/usr/bin/env bash

OUTPUT_DIR=${OUTPUT_DIR:-"_output"}

./${OUTPUT_DIR}/kubeturbo \
	 --v=3 \
	 --master="http://172.17.0.1:8080" \
	 --testingflag="./hack/testing-flag.json" \
	 --turboconfig="./hack/container-conf.json" > "/tmp/kubeturbo.log" 2>&1 &
