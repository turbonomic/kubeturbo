#!/bin/env bash

################## PRE-CONFIGURATION ##################
## Put your Hardcoded ARGS varibles assignments here ##
# TARGET_HOST=""
# OAUTH_CLIENT_ID=""
# OAUTH_CLIENT_SECRET=""
# TSC_TOKEN=""

################## CMD ALIAS ##################
KUBECTL=$(command -v oc)
KUBECTL=${KUBECTL:-$(command -v kubectl)}
if ! [ -x "${KUBECTL}" ]; then
    echo "ERROR: Command 'oc' and 'kubectl' are not found, please install either of them first!" >&2 && exit 1
fi

################## CONSTANT ##################
TSC_TOKEN_FILE=""
DEFAULT_NS="turbo"
DEFAULT_TARGET_NAME="Kubeturbo-non-OCP"
DEFAULT_ROLE="cluster-admin"
DEFAULT_ENABLE_TSC="optional"
DEFAULT_PROXY_SERVER=""
DEFAULT_KUBETURBO_NAME="kubeturbo-release"
DEFAULT_KUBETURBO_VERSION="8.13.1"
DEFAULT_KUBETURBO_REGISTRY="icr.io/cpopen/turbonomic/kubeturbo"

RETRY_INTERVAL=10 # in seconds
MAX_RETRY=10

################## ARGS ##################
ACTION=${ACTION:-"create"}
XL_USERNAME=${XL_USERNAME:-"kubeturbo1"}
XL_PASSWORD=${XL_PASSWORD:-"kubeturbo1"}
OPERATOR_RELEASE=${OPERATOR_RELEASE:-"staging"}

TARGET_HOST=${TARGET_HOST:-""}
OAUTH_CLIENT_ID=${OAUTH_CLIENT_ID:-""}
OAUTH_CLIENT_SECRET=${OAUTH_CLIENT_SECRET:-""}
TSC_TOKEN=${TSC_TOKEN:-""}

OPERATOR_NS=${OPERATOR_NS:-${DEFAULT_NS}}
TARGET_NAME=${TARGET_NAME:-${DEFAULT_TARGET_NAME}}
KUBETURBO_ROLE=${KUBETURBO_ROLE:-${DEFAULT_ROLE}}
ENABLE_TSC=${ENABLE_TSC:-${DEFAULT_ENABLE_TSC}}
PROXY_SERVER=${PROXY_SERVER:-${DEFAULT_PROXY_SERVER}}
KUBETURBO_NAME=${KUBETURBO_NAME:-${DEFAULT_KUBETURBO_NAME}}
KUBETURBO_VERSION=${KUBETURBO_VERSION:-${DEFAULT_KUBETURBO_VERSION}}
KUBETURBO_REGISTRY=${KUBETURBO_REGISTRY:-${DEFAULT_KUBETURBO_REGISTRY}}

################## FUNCTIONS ##################
function validate_args() {
    while [ $# -gt 0 ]; do
        case $1 in
            --host) shift; TARGET_HOST="$1"; [ -n "${TARGET_HOST}" ] && shift;;
            --clientId) shift; OAUTH_CLIENT_ID="$1"; [ -n "${OAUTH_CLIENT_ID}" ] && shift;;
            --clientSecret) shift; OAUTH_CLIENT_SECRET="$1"; [ -n "${OAUTH_CLIENT_SECRET}" ] && shift;;
            --tscTokenFile) shift; TSC_TOKEN_FILE="$1"; [ -n "${TSC_TOKEN_FILE}" ] && shift;;
            -*|--*) echo "ERROR: Unknown option $1" >&2; usage; exit 1;;
            *) shift;;
        esac
    done

    if [[ -n "${TSC_TOKEN}" || -n "${TSC_TOKEN_FILE}" ]]; then ENABLE_TSC="true"; fi
    if [ -z "${TARGET_HOST}" ]; then
        echo "ERROR: Missing target host" >&2; usage; exit 1
    fi
}

function usage() {
   echo "This program helps to install Kubeturbo to the the operator"
   echo "Syntax: ./$0 --host <IP> --clientId <OAUTH CLIENT ID> --clientSecret <OAUTH CLIENT SECRET>"
   echo
   echo "options:"
   echo "--host         <VAL>    host ip      of the Turbonomic instance (required)"
   echo "--clientId     <VAL>    oauth id     of the Turbonomic instance (optional)"
   echo "--clientSecret <VAL>    oauth secret of the Turbonomic instance (optional)"
   echo "--tscTokenFile <VAL>    absoult path of the TSC token response (optional)"
   echo
}

function main() {
    echo "Starting to deploy Kubeturbo in ${OPERATOR_NS} namespace "
    if [[ ${ACTION} != "delete" && ${ENABLE_TSC} == "optional" ]]; then
        read -p "Do you want to install with the auto logging and auto version updates? (Y/n): " enableTSC
        ENABLE_TSC=$([[ "${enableTSC}" == "Y" ]] && echo "true" || echo "false")
    fi
    setup_kubeturbo
    setup_tsc
    echo "Done!"
}

function setup_kubeturbo() {
    if [[ ${ENABLE_TSC} != "true" ]]; then
        apply_oauth2_token
    fi

    if [[ ${ACTION} == "delete" ]]; then
        apply_kubeturbo_cr
        apply_kubeturbo_operator
    else
        apply_kubeturbo_operator
        apply_kubeturbo_cr
    fi

    echo "Successfully ${ACTION} Kubeturbo in ${OPERATOR_NS} namespace!"
    ${KUBECTL} -n ${OPERATOR_NS} get Kubeturbo,pod,deploy
}

function apply_kubeturbo_operator {
    local source_github_repo="https://raw.githubusercontent.com/turbonomic/kubeturbo-deploy"
    local operator_yaml_path="deploy/kubeturbo_operator_yamls/operator-bundle.yaml"
    local operator_yaml_str=$(curl "${source_github_repo}/${OPERATOR_RELEASE}/${operator_yaml_path}" | sed "s/: turbonomic$/: ${OPERATOR_NS}/g")
    ${KUBECTL} ${ACTION} -f - <<< "${operator_yaml_str}"
    wait_for_deployment ${OPERATOR_NS} "kubeturbo-operator"
}

function apply_kubeturbo_cr() {
    echo "${ACTION} Kubeturbo CR ..."
    cat <<-EOF | ${KUBECTL} ${ACTION} -f -
	---
	kind: Kubeturbo
	apiVersion: charts.helm.k8s.io/v1
	metadata:
	  name: ${KUBETURBO_NAME}
	  namespace: ${OPERATOR_NS}
	spec:
	  serverMeta:
	    turboServer: ${TARGET_HOST}
	    version: ${KUBETURBO_VERSION}
	    proxy: ${PROXY_SERVER}
	  targetConfig:
	    targetName: ${TARGET_NAME}
	  image:
	    repository: ${DEFAULT_KUBETURBO_REGISTRY}
	    tag: ${KUBETURBO_VERSION}
	  roleName: ${KUBETURBO_ROLE}
	---
	EOF
    wait_for_deployment ${OPERATOR_NS} ${KUBETURBO_NAME}
}

function apply_oauth2_token {
    if [[ ${ACTION} != "delete" ]]; then 
        if [[ -z "${OAUTH_CLIENT_ID}" || -z "${OAUTH_CLIENT_SECRET}" ]]; then
            echo "Missing OAuth2 client settings, please followe the instruction in the following link: "
            echo "https://github.com/turbonomic/kubeturbo/blob/master/hack/createNewOAuthClient.js"
            read -p "Enable enter your OAuth2 client id: " OAUTH_CLIENT_ID
            read -p "Enable enter your OAuth2 client secret: " OAUTH_CLIENT_SECRET
            apply_oauth2_token && return
        fi
    fi

    cat <<-EOF | ${KUBECTL} ${ACTION} -f -
	apiVersion: v1
	kind: Secret
	metadata:
	  name: turbonomic-credentials
	  namespace: ${OPERATOR_NS}
	type: Opaque
	data:
	  username: $(encode_inline ${XL_USERNAME})
	  password: $(encode_inline ${XL_PASSWORD})
	  clientid: $(encode_inline ${OAUTH_CLIENT_ID})
	  clientsecret: $(encode_inline ${OAUTH_CLIENT_SECRET})
	---
	EOF
}

function setup_tsc() {        
    if [[ ${ACTION} == "delete" ]]; then
        apply_skupper_tunnel
        apply_tsc_cr
        apply_tsc_operator
    else
        if [[ "${ENABLE_TSC}" != "true" ]]; then return; fi
        ${KUBECTL} -n ${OPERATOR_NS} delete secret turbonomic-credentials
        apply_tsc_operator
        apply_tsc_cr
        apply_skupper_tunnel
    fi

    echo "Successfully ${ACTION} TSC in ${OPERATOR_NS} namespace!"
    ${KUBECTL} -n ${OPERATOR_NS} get pod,deploy
}

function apply_tsc_operator {
    local source_github_repo="https://raw.githubusercontent.com/turbonomic/hybrid-saas-client-operator"
    local operator_yaml_path="deploy/tsc_operator_yamls/operator-bundle.yaml"
    local operator_yaml_str=$(curl "${source_github_repo}/${OPERATOR_RELEASE}/${operator_yaml_path}" | sed "s/: turbonomic$/: ${OPERATOR_NS}/g")
    ${KUBECTL} ${ACTION} -f - <<< "${operator_yaml_str}"
    wait_for_deployment ${OPERATOR_NS} "t8c-client-operator-controller-manager"
}

function apply_tsc_cr() {
    echo "${ACTION} TSC CR ..."
    local tsc_client_name="turbonomicclient-release"
    cat <<-EOF | ${KUBECTL} ${ACTION} -f -
	---
	kind: TurbonomicClient
	apiVersion: clients.turbonomic.ibm.com/v1alpha1
	metadata:
	  name: ${tsc_client_name}
	  namespace: ${OPERATOR_NS}
	spec:
	  global:
	    version: 8.12.0
	---
	apiVersion: clients.turbonomic.ibm.com/v1alpha1
	kind: VersionManager
	metadata:
	  name: versionmanager-release
	  namespace: ${OPERATOR_NS}
	spec:
	  url: 'http://remote-nginx-tunnel:9080/cluster-manager/clusterConfiguration'
	---
	EOF
    if [ ${ACTION} != "delete" ]; then
        echo "Waiting for TSC client to be ready ..."
        wait_for_deployment ${OPERATOR_NS} "tsc-site-resources"
        sleep 20 && ${KUBECTL} wait pod \
            -n ${OPERATOR_NS} \
            -l "app.kubernetes.io/part-of=${tsc_client_name}" \
            --for=condition=Ready \
            --timeout=60s
    fi
}

function apply_skupper_tunnel() {
    if [ ${ACTION} == "delete" ]; then 
        echo "${ACTION} secrets for skupper connection ..."
        for it in $(${KUBECTL} get secret -n ${OPERATOR_NS} -l "skupper.io/type" -o name); do
            ${KUBECTL} ${ACTION} -n ${OPERATOR_NS} ${it}
        done
        return
    fi

    if [[ -z "${TSC_TOKEN}" ]]; then
        if [[  -f "${TSC_TOKEN_FILE}" ]]; then
            TSC_TOKEN=$(getJsonField "$(cat "${TSC_TOKEN_FILE}")" "tokenData")
        else
            echo "Please follow the wiki to get the TSC token file: "
            echo "https://ibm-turbo-wiki.fyre.ibm.com/en/CON/XD/XL-Development-Home/Turbonomic-Hybrid-SAAS/TSC-Client-Inst__shift"
            echo "Warning: cannot find TSC token file under: ${TSC_TOKEN_FILE}"
            read -p "Please enter the absoult path for your TSC token: " TSC_TOKEN_FILE
        fi
        apply_skupper_tunnel && return
    fi

    local skupper_connection_secret=$(base64 -d <<< "${TSC_TOKEN}")
    echo "${skupper_connection_secret}" | ${KUBECTL} ${ACTION} -n ${OPERATOR_NS} -f -

    echo "Info: the TSC token expires after certain time, if failed please request anthor token and assign the tokenData to TSC_TOKEN!"
    echo "Waiting for setting up skupper connection..."
    local retry_count=0
    while true; do
        local tunnel_svc=$(${KUBECTL} -n ${OPERATOR_NS} get service -o name | grep remote-nginx-tunnel)
        if [ -n "${tunnel_svc}" ];then return; fi
        ((++ retry_count)) && retry ${retry_count}
    done
}

function wait_for_deployment() {
    if [[ ${ACTION} == "delete" ]]; then return; fi
    local namespace=$1 && local deploy_name=$2

    echo "Waiting for deployment '${deploy_name}' to start..."
    local retry_count=0
    while true; do
        local full_deploy_name=$(${KUBECTL} -n ${namespace} get deploy -o name | grep ${deploy_name})
        if [ -n "${full_deploy_name}" ]; then
            local deploy_status=$(${KUBECTL} -n ${namespace} rollout status ${full_deploy_name} | grep "successfully")
            echo "${deploy_status}"
            if [ -n "${deploy_status}" ]; then
                local deploy_name=$(awk -F '/' '{print $2}' <<< ${full_deploy_name})
                for pod in $(${KUBECTL} -n ${namespace} get pods -o name | grep ${deploy_name}); do
                    ${KUBECTL} -n ${namespace} wait --for=condition=Ready ${pod}
                done
                break
            fi
        fi
        ((++ retry_count)) && retry ${retry_count}
    done
}

function retry() {
    local attempts=${1:--999}
    if [ ${attempts} -ge ${MAX_RETRY} ]; then
        echo "ERROR: Resource is not ready in ${MAX_RETRY} attempts." >&2 && exit 1
    else
        attempt_str=$([ ${attempts} -ge 0 ] && echo " (${attempts}/${MAX_RETRY})")
        echo "Resource is not ready, re-attempt after ${RETRY_INTERVAL}s ...${attempt_str}"
        sleep ${RETRY_INTERVAL}
    fi
}

function getJsonField() {
    jsonData=$1 && field=$2
    if [ -z "$(echo "${jsonData}" | grep ${field})" ]; then
        echo -e "Unable to get field ${field} due to:\n${jsonData}" >&2 && exit 1
    fi
    echo "${jsonData}" | sed -e "s/^{//g" -e "s/}$//g" -e "s/,/\n/g" -e "s/\"//g" | grep ${field} | sed -e "s/[\" ]//g" | awk -F ':' '{print $2}'
}

function encode_inline() {
    local input=$1
    if [[ "$OSTYPE" == "darwin"* ]]; then
        base64 -b 0 <<< $1
    else
        base64 -w 0 <<< $1
    fi
}

################## MAIN ##################
validate_args $@ && main
