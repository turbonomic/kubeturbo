#!/bin/env bash

SCRIPT_DIR="$(cd "$(dirname $0)" && pwd)"
ERR_LOG=$(mktemp "${TMPDIR:-/tmp}/kube_errlog_XXXXXXX")
WAIT_FOR_DEPLOYMENT=20
OPERATOR_IMAGE_VERSION=${OPERATOR_IMG_VERSION-"8.9.5-SNAPSHOT"}
OPERATOR_IMAGE_BASE=${OPERATOR_IMG_BASE-"icr.io/cpopen/kubeturbo-operator"}
OPERATOR_IMAGE="${OPERATOR_IMAGE_BASE}:${OPERATOR_IMAGE_VERSION}"
OPERATOR_IMAGE_STR=$(printf '%s\n' "${OPERATOR_IMAGE}" | sed -e 's/[\/&]/\\&/g')
HOST_IP="127.0.0.1"
KUBETURBO_VERSION=${KUBETURBO_VERSION-"8.9.5-SNAPSHOT"}

# turbonomic is the default namespace that matches the xl setup
# please change the value according to your set up
export namespace=turbonomic

function install_operator {
	NS=$1
	[ -z "${NS}" ] && echo -e "Operator namespace not provided" | tee -a ${ERR_LOG} && exit 1
	kubectl create ns ${NS}
	kubectl delete -f ${SCRIPT_DIR}/deploy/crds/*crd.yaml
	kubectl apply -f ${SCRIPT_DIR}/deploy/crds/*crd.yaml
	kubectl apply -f ${SCRIPT_DIR}/deploy/service_account.yaml -n ${NS}
	kubectl apply -f ${SCRIPT_DIR}/deploy/role_binding.yaml

	# dynamically apply the image version
	sed "s/image:.*/image: ${OPERATOR_IMAGE_STR}/g" ${SCRIPT_DIR}/deploy/operator.yaml > ${SCRIPT_DIR}/deploy/updated_operator.yaml
	kubectl apply -f ${SCRIPT_DIR}/deploy/updated_operator.yaml -n ${NS}
}

function uninstall_operator {
	NS=$1
	[ -z "${NS}" ] && echo -e "Operator namespace not provided" | tee -a ${ERR_LOG} && exit 1

	kubectl delete -f ${SCRIPT_DIR}/deploy/updated_operator.yaml -n ${NS}
	rm ${SCRIPT_DIR}/deploy/updated_operator.yaml

	kubectl delete -f ${SCRIPT_DIR}/deploy/role_binding.yaml
	kubectl delete -f ${SCRIPT_DIR}/deploy/service_account.yaml -n ${NS}
	kubectl delete ns ${NS}
}

function create_cr {
	CR_SURFIX=$1
	CLUSTER_ROLE=${2-"cluster-admin"}

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
	if [ $? -eq 0 ]; then
		# generated testing cr file based on the xl input
		XL_VERSION_DETAIL=$(xl_version)

		HOST_IP=$(echo -e "${XL_VERSION_DETAIL}" | grep "Server:" | awk '{print $2}')
		[ -z "${HOST_IP}" ] && echo -e "Failed to get exposed XL IP" | tee -a ${ERR_LOG} && exit 1

		KUBETURBO_VERSION=$(echo -e "${XL_VERSION_DETAIL}" | grep "Version:" | awk '{print $2}')
		[ -z "${KUBETURBO_VERSION}" ] && echo -e "Failed to get exposed XL version" | tee -a ${ERR_LOG} && exit 1
  fi

	CR_FILEPATH=$(mktemp "${TMPDIR:-/tmp}/kubeturbo_cr_${CR_SURFIX}_XXXXXXX")
	echo ${CR_FILEPATH}

	cat > ${CR_FILEPATH} <<- EOT
	apiVersion: charts.helm.k8s.io/v1
	kind: Kubeturbo
	metadata:
	  name: kubeturbo-${CR_SURFIX}
	spec:
	  serverMeta:
	    version: ${KUBETURBO_VERSION}
	    turboServer: https://${HOST_IP}
	  restAPIConfig:
	    turbonomicCredentialsSecretName: "turbonomic-credentials"
	    opsManagerUserName: ${OPS_MANAGER_USERNAME}
	    opsManagerPassword: ${OPS_MANAGER_PASSWORD}
	  targetConfig:
	    targetName: ${CR_SURFIX}
	  roleName: ${CLUSTER_ROLE}
	  image:
	    tag: ${KUBETURBO_VERSION}
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

	CR_DEPLOY_NAME=$(kubectl get ${CR_DEPLOY} -n ${NS} -o jsonpath={.metadata.name})

	# check if the correct cluster role binding get generated
	TARGET_ROLEBINDING="turbo-all-binding-${CR_DEPLOY_NAME}-${NS}"
	kubectl get ClusterRoleBinding "${TARGET_ROLEBINDING}" >/dev/null
	if [ $? -gt 0 ]; then
		FAILED=$((${FAILED}+1))
		echo -e ">  ClusterRoleBinding ${TARGET_ROLEBINDING} check........FAILED" | tee -a ${ERR_LOG}
	else
		echo -e ">  ClusterRoleBinding ${TARGET_ROLEBINDING} check........PASSED"
	fi

	TARGET_ROLENAME=$(grep roleName ${CR_FILE} | awk '{print $2}')
	if [ -z "${TARGET_ROLENAME}" ]; then
		TARGET_ROLENAME="cluster-admin"
	elif [ "${TARGET_ROLENAME}" != "cluster-admin" ]; then
		TARGET_ROLENAME="${TARGET_ROLENAME}-${CR_DEPLOY_NAME}-${NS}"
	fi
	kubectl get ClusterRole "${TARGET_ROLENAME}" >/dev/null
	if [ $? -gt 0 ]; then
		FAILED=$((${FAILED}+1))
		echo -e ">  ClusterRole ${TARGET_ROLENAME} check........FAILED" | tee -a ${ERR_LOG}
	else
		echo -e ">  ClusterRole ${TARGET_ROLENAME} check........PASSED"
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
	CR_NS3=testns3
	CR_NS4=testns4
	CR_FILE1=$(create_cr testcr1)
	CR_FILE2=$(create_cr testcr2)
	CR_FILE3=$(create_cr testcr3 "turbo-cluster-reader")
	CR_FILE4=$(create_cr testcr4 "turbo-cluster-reader")
	install_cr ${CR_NS1} ${CR_FILE1} "Deploy single kubeturbo test"
	install_cr ${CR_NS2} ${CR_FILE2} "Deploy multiple kubeturbos test"
	install_cr ${CR_NS3} ${CR_FILE3} "Deploy single turbo-cluster-reader kubeturbo test"
	install_cr ${CR_NS4} ${CR_FILE4} "Deploy multiple turbo-cluster-reader kubeturbos test"

	echo -e "> Tearing down kubeturbo tests"
	uninstall_cr ${CR_NS1} ${CR_FILE1}
	uninstall_cr ${CR_NS2} ${CR_FILE2}
	uninstall_cr ${CR_NS3} ${CR_FILE3}
	uninstall_cr ${CR_NS4} ${CR_FILE4}
	uninstall_operator ${OPERATOR_NS}

	TEST_RESULT=$(cat ${ERR_LOG})
	if [ -n "${TEST_RESULT}" ]; then
		echo -e "Kubeturbo test failed see logs at: ${ERR_LOG}" && exit 1
	fi

	rm ${ERR_LOG}
}

main
