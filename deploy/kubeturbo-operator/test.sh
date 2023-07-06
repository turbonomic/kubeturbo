#!/bin/env bash

SCRIPT_DIR="$(cd "$(dirname $0)" && pwd)"
ERR_LOG=$(mktemp --suffix _kube.errlog)
WAIT_FOR_DEPLOYMENT=20

# turbonomic is the default namespace that matches the xl setup
# please change the value according to your set up
export namespace=turbonomic

function install_operator {
	NS=$1
	[ -z "${NS}" ] && echo -e "Operator namespace not provided" | tee -a ${ERR_LOG} && exit 1
	kubectl create ns ${NS}
	kubectl apply -f ${SCRIPT_DIR}/deploy/crds/*crd.yaml
	kubectl apply -f ${SCRIPT_DIR}/deploy/service_account.yaml -n ${NS}
	kubectl apply -f ${SCRIPT_DIR}/deploy/role_binding.yaml
	kubectl apply -f ${SCRIPT_DIR}/deploy/operator.yaml -n ${NS}
}

function uninstall_operator {
	NS=$1
	[ -z "${NS}" ] && echo -e "Operator namespace not provided" | tee -a ${ERR_LOG} && exit 1
	kubectl delete -f ${SCRIPT_DIR}/deploy/operator.yaml -n ${NS}
	kubectl delete -f ${SCRIPT_DIR}/deploy/role_binding.yaml
	kubectl delete -f ${SCRIPT_DIR}/deploy/service_account.yaml -n ${NS}
	kubectl delete ns ${NS}
}

function create_cr {
	CR_SURFIX=$1

	# username and password for the local ops-manager
	OPS_MANAGER_USERNAME=administrator
	OPS_MANAGER_PASSWORD=administrator

	# if we have .turbocreds pull the credentials from it instead
	FILE="~/.turbocreds"
	if [ -f "${FILE}" ]; then
		if [[ $(grep -c "opsman_username" ${FILE}) -eq 1 ]]; then
			OPS_MANAGER_USERNAME=`grep "opsman_username" ${FILE} | cut -d'=' -f2`
			OPS_MANAGER_PASSWORD=`grep "opsman_password" ${FILE} | cut -d'=' -f2`
		fi
	fi

	command -v xl_version &> /dev/null
	[ $? -gt 0 ] && echo -e "Failed to invoke xl_version from environemnt" | tee -a ${ERR_LOG} && exit 1
	XL_VERSION_DETAIL=$(xl_version)

	# generated testing cr file based on the given input
	HOST_IP=$(echo -e "${XL_VERSION_DETAIL}" | grep "Server:" | awk '{print $2}')
	[ -z "${HOST_IP}" ] && echo -e "Failed to get exposed XL IP" | tee -a ${ERR_LOG} && exit 1

	XL_VERSION=$(echo -e "${XL_VERSION_DETAIL}" | grep "Version:" | awk '{print $2}')
	[ -z "${XL_VERSION}" ] && echo -e "Failed to get exposed XL version" | tee -a ${ERR_LOG} && exit 1

	CR_FILEPATH=$(mktemp --suffix _kubeturbo_cr_${CR_SURFIX}.yaml)
	echo ${CR_FILEPATH}

	cat > ${CR_FILEPATH} <<- EOT
	apiVersion: charts.helm.k8s.io/v1
	kind: Kubeturbo
	metadata:
	  name: kubeturbo-${CR_SURFIX}
	spec:
	  serverMeta:
	    version: ${XL_VERSION}
	    turboServer: https://${HOST_IP}
	  restAPIConfig:
	    turbonomicCredentialsSecretName: "turbonomic-credentials"
	    opsManagerUserName: ${OPS_MANAGER_USERNAME}
	    opsManagerPassword: ${OPS_MANAGER_PASSWORD}
	  targetConfig:
	    targetName: ${CR_SURFIX}
	EOT
}

function install_cr {
	# install cr in a given namespace and run checks
	NS=$1
	CR_FILE=$2
	TEST_DESC=${3-"Install Kubeturbo CR in ${NS}"}

	[ -z "${NS}" ] && echo "Namespace not provided" | tee -a ${ERR_LOG} && return
	[ ! -f "${CR_FILE}" ] && echo "CR file ${CR_FILE} not provided" | tee -a ${ERR_LOG} && return

	echo -e "> Start testing for ${TEST_DESC}"
	kubectl create ns ${NS}
	kubectl apply -f ${CR_FILE} -n ${NS} && echo -e "Wait for ${WAIT_FOR_DEPLOYMENT}s to finish container creation"
	sleep ${WAIT_FOR_DEPLOYMENT}

	# check if the kubeturbo deployment get generated
	FAILED=0
	CR_DEPLOY=$(kubectl get deploy -n ${NS} -o NAME)
	if [ -z "${CR_DEPLOY}" ]; then
		echo -e "> ${TEST_DESC}........FAILED" | tee -a ${ERR_LOG}
		return
	elif [ -z "$(kubectl rollout status ${CR_DEPLOY} -n ${NS} | grep successfully)" ]; then
		FAILED=$((${FAILED}+1))
		echo -e ">  Deployment ${CR_DEPLOY} cr check........FAILED" | tee -a ${ERR_LOG}
	else
		echo -e ">  Deployment ${CR_DEPLOY} cr check........PASSED"
	fi

	# check if the cluster log is healthy
	if [ $(kubectl logs ${CR_DEPLOY} -n ${NS} | grep "Successfully" | wc -l) -lt 2 ]; then
		FAILED=$((${FAILED}+1))
		echo -e ">  Deployment ${CR_DEPLOY} log check........FAILED" | tee -a ${ERR_LOG}
	else
		echo -e ">  Deployment ${CR_DEPLOY} log check........PASSED"
	fi

	# check if the correct cluster role binding get generated
	CR_DEPLOY_NAME=$(kubectl get ${CR_DEPLOY} -n ${NS} -o jsonpath={.metadata.name})
	TARGET_ROLEBINDING="turbo-all-binding-${CR_DEPLOY_NAME}-${NS}"
	if [ -z $(kubectl get ClusterRoleBinding -o NAME | grep ${TARGET_ROLEBINDING}) ]; then
		FAILED=$((${FAILED}+1))
		echo -e ">  ClusterRoleBinding ${TARGET_ROLEBINDING} check........FAILED" | tee -a ${ERR_LOG}
	else
		echo -e ">  ClusterRoleBinding check........PASSED"
	fi

	# summarize the deployment test
	if [ ${FAILED} -gt 0 ]; then
		echo -e "> ${TEST_DESC}........FAILED" | tee -a ${ERR_LOG}
	else
		echo -e "> ${TEST_DESC}........PASSED"
	fi
}

function uninstall_cr {
	# unstall cr in a given namespace
	NS=$1
	CR_FILE=$2

	if [ -f "${CR_FILE}" ] && [ -n "${NS}" ]; then
		kubectl delete -f ${CR_FILE} -n ${NS}
	fi

	[ -f "${CR_FILE}" ] && rm ${CR_FILE}
	[ -n "${NS}" ] && kubectl delete ns ${NS}
}


function main {
	echo -e "> Tearing up kubeturbo tests"

	# turbo is the default namespace that matches the one using in
	# deploy/role_binding.yaml, please change the value accordingly
	OPERATOR_NS=turbo
	install_operator ${OPERATOR_NS}

	CR_NS1=testns1
	CR_NS2=testns2
	CR_FILE1=$(create_cr testcr1)
	CR_FILE2=$(create_cr testcr2)
	install_cr ${CR_NS1} ${CR_FILE1} "Deploy single kubeturbo test"
	install_cr ${CR_NS2} ${CR_FILE2} "Deploy multiple kubeturbos test"

	echo -e "> Tearing down kubeturbo tests"
	uninstall_cr ${CR_NS1} ${CR_FILE1}
	uninstall_cr ${CR_NS2} ${CR_FILE2}
	uninstall_operator ${OPERATOR_NS}

	TEST_RESULT=$(cat ${ERR_LOG})
	if [ -n "${TEST_RESULT}" ]; then
		echo -e "Kubeturbo test failed see logs at: ${ERR_LOG}" && exit 1
	fi

	rm ${ERR_LOG}
}

main

