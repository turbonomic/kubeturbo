#!/usr/bin/env bash

################## CMD ALIAS ##################
KUBECTL=$(command -v oc)
KUBECTL=${KUBECTL:-$(command -v kubectl)}
if ! [ -x "${KUBECTL}" ]; then
    echo "ERROR: Command 'oc' and 'kubectl' are not found, please install either of them first!" >&2 && exit 1
fi

################## PRE-CONFIGURATION ##################
## Put your Hardcoded ARGS varibles assignments here ##
# TARGET_HOST=""
# OAUTH_CLIENT_ID=""
# OAUTH_CLIENT_SECRET=""
# TSC_TOKEN=""

################## CONSTANT ##################
CARALOG_SOURCE="certified-operators"
CARALOG_SOURCE_NS="openshift-marketplace"

TSC_TOKEN_FILE=""
DEFAULT_RELEASE="stable"
DEFAULT_NS="turbo"
DEFAULT_TARGET_NAME="Kubeturbo-OCP"
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
TARGET_RELEASE=${TARGET_RELEASE:-${DEFAULT_RELEASE}}

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

################## DYNAMIC VARS ##################
CERT_OP_NAME="<EMPTY>"
CERT_OP_RELEASE="<EMPTY>"
CERT_OP_VERSION="<EMPTY>"
CERT_KUBETURBO_OP_NAME="<EMPTY>"
CERT_KUBETURBO_OP_RELEASE="<EMPTY>"
CERT_KUBETURBO_OP_VERSION="<EMPTY>"
CERT_TSC_OP_NAME="<EMPTY>"
CERT_TSC_OP_RELEASE="<EMPTY>"
CERT_TSC_OP_VERSION="<EMPTY>"

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
   echo "This program helps to install Kubeturbo to the cluster via the OperatorHub"
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
    echo "Creating ${OPERATOR_NS} namespace to deploy Certified Kubeturbo operator"
    if [[ ${ACTION} != "delete" && ${ENABLE_TSC} == "optional" ]]; then
        read -p "Do you want to install with the auto logging and auto version updates? (Y/n): " enableTSC
        ENABLE_TSC=$([[ "${enableTSC}" == "Y" ]] && echo "true" || echo "false")
    fi
    ${KUBECTL} create ns ${OPERATOR_NS}
    apply_operator_group
    setup_kubeturbo
    setup_tsc
    echo "Done!"
}

function setup_kubeturbo() {
    if [[ ${ENABLE_TSC} != "true" ]]; then
        apply_oauth2_token
    fi

    select_cert_op_from_operatorhub "kubeturbo"
    select_cert_op_channel_from_operatorhub
    CERT_KUBETURBO_OP_NAME=${CERT_OP_NAME}
    CERT_KUBETURBO_OP_RELEASE=${CERT_OP_RELEASE}
    CERT_KUBETURBO_OP_VERSION=${CERT_OP_VERSION}

    if [ ${ACTION} == "delete" ]; then
        apply_kubeturbo_cr
        apply_kubeturbo_op_subscription
    else
        apply_kubeturbo_op_subscription
        apply_kubeturbo_cr
    fi

    echo "Successfully ${ACTION} Kubeturbo in ${OPERATOR_NS} namespace!"
    ${KUBECTL} -n ${OPERATOR_NS} get OperatorGroup,Subscription,Kubeturbo,pod,deploy
}

function setup_tsc() {
    select_cert_op_from_operatorhub "t8c-tsc"
    select_cert_op_channel_from_operatorhub
    CERT_TSC_OP_NAME=${CERT_OP_NAME}
    CERT_TSC_OP_RELEASE=${CERT_OP_RELEASE}
    CERT_TSC_OP_VERSION=${CERT_OP_VERSION}

    if [ ${ACTION} == "delete" ]; then
        apply_skupper_tunnel
        apply_tsc_cr
        apply_tsc_op_subscription
    else
        if [[ "${ENABLE_TSC}" != "true" ]]; then return; fi
        ${KUBECTL} -n ${OPERATOR_NS} delete secret turbonomic-credentials
        apply_tsc_op_subscription
        apply_tsc_cr
        apply_skupper_tunnel
    fi
    echo "Successfully ${ACTION} TSC operator in the ${OPERATOR_NS} namespace!"
}

function select_cert_op_from_operatorhub() {
    local target=$1
    echo "Fetching Openshift certified ${target} operator from OperatorHub ..."
    local cert_ops=$(${KUBECTL} get packagemanifests -o jsonpath="{range .items[*]}{.metadata.name} {.status.catalogSource} {.status.catalogSourceNamespace}{'\n'}{end}" | grep -e ${target} | grep -e "${CARALOG_SOURCE}.*${CARALOG_SOURCE_NS}" | awk '{print $1}')
    local cert_ops_count=$(echo "${cert_ops}" | wc -l | awk '{print $1}')
    if [ -z ${cert_ops} ] || [ ${cert_ops_count} -lt 1 ]; then
        echo "There aren't any certified ${target} operator in the Operatorhub, please contact administrator for more information!" && exit 1
    elif [ ${cert_ops_count} -gt 1 ]; then
        PS3="Fetched mutiple certified ${target} operators in the Operatorhub, please select a number to proceed OR type 'exit' to exit: "
        select opt in ${cert_ops[@]}; do
            validate_select_input ${cert_ops_count} ${REPLY}
            if [ $? -eq 0 ]; then
                cert_ops=${opt}
                break;
            fi
        done
    fi
    CERT_OP_NAME=${cert_ops}
    echo "Using Openshift certified ${target} operator: ${CERT_OP_NAME}"
}

function select_cert_op_channel_from_operatorhub() {
    local cert_op_name=${1-${CERT_OP_NAME}}
    echo "Fetching Openshift ${cert_op_name} channels from OperatorHub ..."
    local channels=$(${KUBECTL} get packagemanifests ${cert_op_name} -o jsonpath="{range .status.channels[*]}{.name}:{.currentCSV}{'\n'}{end}" | grep "${TARGET_RELEASE}")
    local channel_count=$(echo "${channels}" | wc -l | awk '{print $1}')
    if [ -z "${channels}" ] || [ ${channel_count} -lt 1 ]; then
        echo "There aren't any channel created for ${cert_op_name}, please contact administrator for more information!" && exit 1
    elif [ ${channel_count} -gt 1 ]; then
        PS3="Fetched mutiple releases, please select a number to proceed OR type 'exit' to exit: "
        select opt in ${channels[@]}; do
            validate_select_input ${channel_count} ${REPLY}
            if [ $? -eq 0 ]; then
                channels=${opt}
                break;
            fi
        done
    fi
    CERT_OP_RELEASE=$(echo ${channels} | awk -F':' '{print $1}')
    CERT_OP_VERSION=$(echo ${channels} | awk -F':' '{print $2}')
    echo "Using Openshift certified ${cert_op_name} ${CERT_OP_RELEASE} channel, version ${CERT_OP_VERSION}"
}

function apply_operator_group() {
    op_gp_count=$(${KUBECTL} -n ${OPERATOR_NS} get OperatorGroup -o name | wc -l)
    if [ ${op_gp_count} -eq 1 ]; then 
        return
    elif [ ${op_gp_count} -gt 1 ]; then 
        echo "ERROR: Found multiple Operator Groups in the namespace ${OPERATOR_NS}" >&2 && exit 1
    fi
    cat <<-EOF | ${KUBECTL} ${ACTION} -f -
	---
	apiVersion: operators.coreos.com/v1
	kind: OperatorGroup
	metadata:
	  name: kubeturbo-opeartorgroup
	  namespace: ${OPERATOR_NS}
	spec:
	  targetNamespaces:
	  - ${OPERATOR_NS}
	---
	EOF
}

function apply_kubeturbo_op_subscription() {
    echo "${ACTION} Certified Kubeturbo operator subscription ..."
    if [ ${ACTION} == "delete" ]; then
        ${KUBECTL} -n ${OPERATOR_NS} delete Subscription ${CERT_KUBETURBO_OP_NAME}
        ${KUBECTL} -n ${OPERATOR_NS} delete csv ${CERT_KUBETURBO_OP_VERSION}
        return
    fi
    cat <<-EOF | ${KUBECTL} ${ACTION} -f -
	---
	apiVersion: operators.coreos.com/v1alpha1
	kind: Subscription
	metadata:
	  name: ${CERT_KUBETURBO_OP_NAME}
	  namespace: ${OPERATOR_NS}
	spec:
	  channel: ${CERT_KUBETURBO_OP_RELEASE}
	  installPlanApproval: Automatic
	  name: ${CERT_KUBETURBO_OP_NAME}
	  source: ${CARALOG_SOURCE}
	  sourceNamespace: ${CARALOG_SOURCE_NS}
	  startingCSV: ${CERT_KUBETURBO_OP_VERSION}
	---
	EOF
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

function apply_tsc_op_subscription() {
    echo "${ACTION} Certified t8c-tsc operator subscription ..."
    if [ ${ACTION} == "delete" ]; then
        ${KUBECTL} -n ${OPERATOR_NS} delete Subscription ${CERT_TSC_OP_NAME}
        ${KUBECTL} -n ${OPERATOR_NS} delete csv ${CERT_TSC_OP_VERSION}
        return
    fi
    cat <<-EOF | ${KUBECTL} ${ACTION} -f -
	---
	apiVersion: operators.coreos.com/v1alpha1
	kind: Subscription
	metadata:
	  name: ${CERT_TSC_OP_NAME}
	  namespace: ${OPERATOR_NS}
	spec:
	  channel: ${CERT_TSC_OP_RELEASE}
	  installPlanApproval: Automatic
	  name: ${CERT_TSC_OP_NAME}
	  source: ${CARALOG_SOURCE}
	  sourceNamespace: ${CARALOG_SOURCE_NS}
	  startingCSV: ${CERT_TSC_OP_VERSION}
	---
	EOF
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

function getJsonField() {
    jsonData=$1 && field=$2
    if [ -z "$(echo "${jsonData}" | grep ${field})" ]; then
        echo -e "Unable to get field ${field} due to:\n${jsonData}" >&2 && exit 1
    fi
    echo "${jsonData}" | sed -e "s/^{//g" -e "s/}$//g" -e "s/,/\n/g" -e "s/\"//g" | grep ${field} | sed -e "s/[\" ]//g" | awk -F ':' '{print $2}'
}

function validate_select_input() {
    local opts_count=$1 && local opt=$2
    if [ "${opt}" == "exit" ]; then
        echo "Exiting the program ..." >&2 && exit 0
    elif ! [[ "${opt}" =~ ^[1-9][0-9]*$ ]]; then
        echo "ERROR: Input not a number: ${opt}" >&2 && exit 1
    elif [ ${opt} -le 0 ] || [ ${opt} -gt ${opts_count} ]; then
        echo "ERROR: Input out of range [1 - ${opts_count}]: ${opt}" >&2 && exit 1
    fi
}

function wait_for_deployment() {
    if [ ${ACTION} == "delete" ]; then return; fi
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
