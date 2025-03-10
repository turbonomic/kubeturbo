#!/usr/bin/env bash
# combine all commands related to kind integration test into one script since travis only allow single line commmands for job.include.script
set -e
./hack/create_kind_cluster.sh
./hack/deploy_istio.sh
./build/integration.test -k8s-kubeconfig=$HOME/.kube/config -k8s-context=kind-kind -ginkgo.skip="Wiremock Tests" -docker-user-name=${DOCKER_USERNAME} -docker-user-password=${DOCKER_PASSWORD}
./build/integration.test -k8s-kubeconfig=$HOME/.kube/config -k8s-context=kind-kind -istio-enabled=true -ginkgo.focus=Istio -ginkgo.focus=teardown -docker-user-name=${DOCKER_USERNAME} -docker-user-password=${DOCKER_PASSWORD}