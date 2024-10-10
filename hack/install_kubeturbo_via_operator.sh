#!/usr/bin/env sh

################## PRE-CONFIGURATION ##################
## Place your hardcoded ARGS variable assignments here ##
# TARGET_HOST=""
# OAUTH_CLIENT_ID=""
# OAUTH_CLIENT_SECRET=""
# TSC_TOKEN=""

################## CMD ALIAS ##################
DEPENDENCY_LIST="grep cat sleep wc awk sed base64 mktemp curl"
KUBECTL=$(command -v oc)
KUBECTL=${KUBECTL:-$(command -v kubectl)}
if ! [ -x "${KUBECTL}" ]; then
    echo "ERROR: Command 'oc' and 'kubectl' are not found, please install either of them first!" >&2 && exit 1
fi

################## CONSTANT ##################
export KUBECTL_WARNINGS="false"

K8S_TYPE="Kubernetes"
OCP_TYPE="RedHatOpenShift"

CARALOG_SOURCE="certified-operators"
CARALOG_SOURCE_NS="openshift-marketplace"

TSC_TOKEN_FILE=""
DEFAULT_RELEASE="stable"
DEFAULT_NS="turbo"
DEFAULT_TARGET_NAME="Customer-cluster"
DEFAULT_ROLE="cluster-admin"
DEFAULT_ENABLE_TSC="optional"
DEFAULT_PROXY_SERVER=""
DEFAULT_KUBETURBO_NAME="kubeturbo-release"
DEFAULT_KUBETURBO_VERSION="8.13.1"
DEFAULT_KUBETURBO_REGISTRY="icr.io/cpopen/turbonomic/kubeturbo"
DEFAULT_LOGGING_LEVEL=0

RETRY_INTERVAL=10 # in seconds
MAX_RETRY=10

################## ARGS ##################
KUBECONFIG=${KUBECONFIG:-""}

ACTION=${ACTION:-"apply"}
KT_TARGET_RELEASE=${KT_TARGET_RELEASE:-${DEFAULT_RELEASE}}
TSC_TARGET_RELEASE=${TSC_TARGET_RELEASE:-${DEFAULT_RELEASE}}
OPERATOR_RELEASE=${OPERATOR_RELEASE:-"staging"}
PWD_SECRET_ENCODED=${PWD_SECRET_ENCODED:-"true"}

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
KUBETURBO_REGISTRY_USRNAME=${KUBETURBO_REGISTRY_USRNAME:-""}
KUBETURBO_REGISTRY_PASSWRD=${KUBETURBO_REGISTRY_PASSWRD:-""}
TARGET_SUBTYPE=${TARGET_SUBTYPE:-""}

LOGGING_LEVEL=${LOGGING_LEVEL:-${DEFAULT_LOGGING_LEVEL}}

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
K8S_CLUSTER_KINDS="<EMPTY>"

################## FUNCTIONS ##################
# check if the current system supports all the commands needed to run the script
dependencies_check() {
    missing_dependencies=""
    for dependency in ${DEPENDENCY_LIST}; do 
        dependency_path=$(command -v ${dependency})
        if ! [ -x "${dependency_path}" ]; then
            missing_dependencies="${missing_dependencies} ${dependency}"
        fi
    done

    if [ -n "${missing_dependencies}" ]; then 
        echo "ERROR: Missing the required command: $(echo ${missing_dependencies} | sed 's/ /, /g')"
        echo "Please refer to the official documentation or use your package manager to install to continue."
        exit 1
    fi
}

validate_args() {
    while [ $# -gt 0 ]; do
        case $1 in
            --host) shift; TARGET_HOST="$1"; [ -n "${TARGET_HOST}" ] && shift;;
            --kubeconfig) shift; KUBECONFIG="$1"; [ -n "${KUBECONFIG}" ] && shift;;
            -*|--*) echo "ERROR: Unknown option $1" >&2; usage; exit 1;;
            *) shift;;
        esac
    done

    if [ -z "${TARGET_HOST}" ]; then
        echo "ERROR: Missing target host" >&2; usage; exit 1
    fi

    if [ -n "${KUBECONFIG}" ]; then
        KUBECTL="${KUBECTL} --kubeconfig=${KUBECONFIG}"
    fi
    KUBECTL="${KUBECTL} --logtostderr=false -v=${LOGGING_LEVEL}"

    # Prioritize the TSC deployment approach once the value is set
    if [ "${ENABLE_TSC}" = "optional" ]; then
        if [ -n "${TSC_TOKEN}" ] || [ -n "${TSC_TOKEN_FILE}" ]; then ENABLE_TSC="true"; fi
    fi

    # Pre-process the encoded secrets and passwords
    if [ "${PWD_SECRET_ENCODED}" = "true" ]; then
        KUBETURBO_REGISTRY_PASSWRD=$(password_secret_handler "${KUBETURBO_REGISTRY_PASSWRD}")
        OAUTH_CLIENT_ID=$(password_secret_handler "${OAUTH_CLIENT_ID}")
        OAUTH_CLIENT_SECRET=$(password_secret_handler "${OAUTH_CLIENT_SECRET}")
        PROXY_SERVER=$(password_secret_handler $PROXY_SERVER)
    fi
}

usage() {
   echo "This program helps to install Kubeturbo to the Kubernetes cluster"
   echo "Syntax: ./$0 --host <IP> --kubeconfig <PATH>"
   echo
   echo "options:"
   echo "--host         <VAL>    host of the Turbonomic instance (required)"
   echo "--kubeconfig   <VAL>    Path to the kubeconfig file to use for CLI requests"
   echo
}

# confirm args that are passed into the script and get user's consent
confirm_installation() {
    proxy_server_enabled=$([ -n "${PROXY_SERVER}" ] && echo 'true' || echo 'false')
    if [ -n "${KUBETURBO_REGISTRY_USRNAME}" ] && [ -n "${KUBETURBO_REGISTRY_PASSWRD}" ]; then
        private_registry_enabled="true"
    else
        private_registry_enabled="false"
    fi
    echo "Here is the summary for the current installation:"
    echo ""
    printf "%-20s %-20s\n" "---------" "---------"
    printf "%-20s %-20s\n" "Parameter" "Value"
    printf "%-20s %-20s\n" "---------" "---------"
    printf "%-20s %-20s\n" "Mode" "$([ ${ACTION} = 'delete' ] && echo 'Delete' || echo 'Create/Update')"
    printf "%-20s %-20s\n" "Kubeconfig" "${KUBECONFIG:-default}"
    printf "%-20s %-20s\n" "Host" "${TARGET_HOST}"
    printf "%-20s %-20s\n" "Namespace" "${OPERATOR_NS}"
    printf "%-20s %-20s\n" "Target Name" "${TARGET_NAME}"
    printf "%-20s %-20s\n" "Target Subtype" "${TARGET_SUBTYPE}"
    printf "%-20s %-20s\n" "Role" "${KUBETURBO_ROLE}"
    printf "%-20s %-20s\n" "Version" "${KUBETURBO_VERSION}"
    printf "%-20s %-20s\n" "Auto-Update" "${ENABLE_TSC}"
    printf "%-20s %-20s\n" "Auto-Logging" "${ENABLE_TSC}"
    printf "%-20s %-20s\n" "Proxy Server" "${proxy_server_enabled}"
    printf "%-20s %-20s\n" "Private Registry" "${private_registry_enabled}"
    echo ""
    read -p "Please confirm the above settings [Y/n]: " continueInstallation
    [ "${continueInstallation}" = "n" ] || [ "${continueInstallation}" = "N" ] && echo "Please retry the script with correct settings!" && exit 1
    cluster_type_check
}

# To determine whether the current kubectl context is an Openshift cluster
cluster_type_check() {
    confirm_installation_cluster

    if [ -z "${TARGET_SUBTYPE}" ]; then
        TARGET_SUBTYPE=$(auto_detect_cluster_type)
        return
    fi
    
    current_context=$(${KUBECTL} config current-context)
    current_oc_cluster_type=$(auto_detect_cluster_type)
    TARGET_SUBTYPE=$(normalize_target_cluster_type ${TARGET_SUBTYPE})
    if [ "${current_oc_cluster_type}" != "${TARGET_SUBTYPE}" ]; then 
        echo "Your current cluster type [${current_oc_cluster_type}] mismatches with the target type [${TARGET_SUBTYPE}] you specified from the UI!"
        read -p "Do you want to continue the installation as the [${current_oc_cluster_type}] target? [y/N]: " allowMismatch
        if [ "${allowMismatch}" = "y" ] || [ "${allowMismatch}" = "Y" ]; then 
            TARGET_SUBTYPE="${current_oc_cluster_type}"
        else
            echo "Please double check your current Kubernetes context before the other try!" && exit 1
        fi
    fi
}

# get client's concent to install to the current cluster
confirm_installation_cluster() {
    echo "Info: Your current Kubernetes context is set to the following:"
    show_current_kube_context
    read -p "Please confirm if the script should work in the above cluster [Y/n]: " continueInstallation
    [ "${continueInstallation}" = "n" ] || [ "${continueInstallation}" = "N" ] && echo "Please double check your current Kubernetes context before the other try!" && exit 1
}

# display current kubeconfig context in a table format
show_current_kube_context() {
    # exit if the current context is not set
    current_context=$(${KUBECTL} config current-context)
    if [ $? -ne 0 ]; then
        echo "ERROR: Current context is not set in your cluster!"
        exit 1
    fi

    # get detail from the raw oject
    context_settings=$(${KUBECTL} config view -o jsonpath='{.contexts[?(@.name == "'$current_context'")].context}')
    cluster=$(${KUBECTL} config view -o jsonpath='{.contexts[?(@.name == "'$current_context'")].context.cluster}')
    user=$(${KUBECTL} config view -o jsonpath='{.contexts[?(@.name == "'$current_context'")].context.user}')
    namespace=$(${KUBECTL} config view -o jsonpath='{.contexts[?(@.name == "'$current_context'")].context.namespace}')

    # print out in the table
    spacing=5
    format="%-$(($(echo ${current_context} | wc -c)+${spacing}))s %-$(($(echo ${cluster} | wc -c)+${spacing}))s %-$(($(echo ${user} | wc -c)+${spacing}))s %-$(($(echo ${namespace} | wc -c)+${spacing}))s\n"
    printf "${format}" "NAME" "CLUSTER" "AUTHINFO" "NAMESPACE"
    printf "${format}" "${current_context}" "${cluster}" "${user}" "${namespace}"

    # exit if the current the cluster is not reachable
    nodes=$(${KUBECTL} get nodes)
    if [ $? -ne 0 ]; then
        echo "ERROR: Context used by the current cluster is not reachable!"
        exit 1
    fi
}

# detech the cluster type
auto_detect_cluster_type() {
    is_current_oc_cluster=$(${KUBECTL} api-resources --api-group=route.openshift.io -o name)
    normalize_target_cluster_type $([ -n "${is_current_oc_cluster}" ] && echo "${OCP_TYPE}")
}

# normalize cluster type to either Openshift or Kubernetes
normalize_target_cluster_type() {
    cluster_type=$1
    is_target_oc_cluster=$(echo "${cluster_type}" | grep -i "${OCP_TYPE}")
    [ -n "${is_target_oc_cluster}" ] && echo "${OCP_TYPE}" || echo "${K8S_TYPE}"
}

main() {
    # gather all cluster level resource kinds
    K8S_CLUSTER_KINDS=$(${KUBECTL} api-resources --namespaced=false --no-headers | awk '{print $NF}')

    NS_EXISTS=$(${KUBECTL} get ns --field-selector=metadata.name=${OPERATOR_NS} -o name)
    if [ -z "${NS_EXISTS}" ]; then
        echo "Creating ${OPERATOR_NS} namespace to deploy Kubeturbo operator"
        ${KUBECTL} create ns ${OPERATOR_NS}
    fi
    
    if [ ${ACTION} != "delete" ] && [ ${ENABLE_TSC} = "optional" ]; then
        read -p "Do you want to install with the auto logging and auto version updates? [Y/n]: " enableTSC
        if [ "${enableTSC}" = "n" ] || [ "${enableTSC}" = "N" ]; then
            ENABLE_TSC="false"
        else
            ENABLE_TSC="true"
        fi
    fi
    
    apply_operator_group
    setup_kubeturbo

    # applicable scenario: user switch from tsc approach to oauth2 approach 
    is_tsc_launched=$(${KUBECTL} -n ${OPERATOR_NS} get deploy --field-selector=metadata.name=t8c-client-operator-controller-manager -o name)
    if [ -n "${is_tsc_launched}" ] && [ "${ENABLE_TSC}" = "false" ]; then
        echo "Info: Dismounting Auto-logging & Auto-updating feature as no longer required ..."
        ACTION="delete"
    fi

    setup_tsc

    echo "Done!"
    exit 0
}

apply_operator_group() {
    if [ "${TARGET_SUBTYPE}" != "${OCP_TYPE}" ]; then return; fi
    op_gp_count=$(${KUBECTL} -n ${OPERATOR_NS} get OperatorGroup -o name | wc -l)
    if [ ${op_gp_count} -eq 1 ]; then 
        return
    elif [ ${op_gp_count} -gt 1 ]; then 
        echo "ERROR: Found multiple Operator Groups in the namespace ${OPERATOR_NS}" >&2 && exit 1
    fi

    action="${ACTION}"
    if [ ${ACTION} = "delete" ]; then
        action="${ACTION} --ignore-not-found"
    fi

    cat <<-EOF | ${KUBECTL} ${action} -f -
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

setup_kubeturbo() {
    if [ ${ENABLE_TSC} != "true" ]; then
        apply_oauth2_token
    fi

    if [ ${ACTION} = "delete" ]; then
        apply_kubeturbo_cr
        apply_kubeturbo_op
    else
        apply_kubeturbo_op
        apply_kubeturbo_cr
    fi

    echo "Successfully ${ACTION} Kubeturbo in ${OPERATOR_NS} namespace!"
    ${KUBECTL} -n ${OPERATOR_NS} get role,rolebinding,sa,pod,deploy,cm -l 'app.kubernetes.io/created-by in (kubeturbo-deploy, kubeturbo-operator)'
}

apply_kubeturbo_op() {
    if [ "${TARGET_SUBTYPE}" = "${OCP_TYPE}" ]; then
        apply_kubeturbo_op_subscription
    else
        apply_kubeturbo_op_yaml
    fi
}

apply_kubeturbo_op_subscription() {
    select_cert_op_from_operatorhub "kubeturbo"
    CERT_KUBETURBO_OP_NAME=${CERT_OP_NAME}

    select_cert_op_channel_from_operatorhub ${CERT_KUBETURBO_OP_NAME} ${KT_TARGET_RELEASE}
    CERT_KUBETURBO_OP_RELEASE=${CERT_OP_RELEASE}
    CERT_KUBETURBO_OP_VERSION=${CERT_OP_VERSION}

    action="${ACTION}"
    if [ ${ACTION} = "delete" ]; then
        action="${ACTION} --ignore-not-found"
    fi

    echo "${ACTION} Certified Kubeturbo operator subscription ..."
    if [ ${ACTION} = "delete" ]; then
        ${KUBECTL} -n ${OPERATOR_NS} ${action} Subscription ${CERT_KUBETURBO_OP_NAME}
        ${KUBECTL} -n ${OPERATOR_NS} ${action} csv ${CERT_KUBETURBO_OP_VERSION}
        return
    fi
    cat <<-EOF | ${KUBECTL} ${action} -f -
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

apply_kubeturbo_op_yaml() {
    operator_deploy_name="kubeturbo-operator"
    operator_service_account="kubeturbo-operator"
    
    source_github_repo="https://raw.githubusercontent.com/turbonomic/kubeturbo-deploy"
    operator_yaml_path="deploy/kubeturbo_operator_yamls/operator-bundle.yaml"
    operator_yaml_bundle=$(curl "${source_github_repo}/${OPERATOR_RELEASE}/${operator_yaml_path}" | sed "s/: turbonomic$/: ${OPERATOR_NS}/g" | sed '/^\s*#/d')
    
    apply_operator_bundle "${operator_service_account}" "${operator_deploy_name}" "${operator_yaml_bundle}"
}

apply_kubeturbo_cr() {
    action="${ACTION}"
    if [ ${ACTION} = "delete" ]; then
        action="${ACTION} --ignore-not-found"
    fi

    echo "${ACTION} Kubeturbo CR ..."
    private_docker_registry=""
    if [ -n "${KUBETURBO_REGISTRY_USRNAME}" ] && [ -n "${KUBETURBO_REGISTRY_PASSWRD}" ]; then
        private_docker_registry="private-docker-registry"
        ${KUBECTL} ${action} secret docker-registry ${private_docker_registry}
        ${KUBECTL} create secret docker-registry ${private_docker_registry} \
            --docker-username=${KUBETURBO_REGISTRY_USRNAME} \
            --docker-password=${KUBETURBO_REGISTRY_PASSWRD} \
            --docker-server=${KUBETURBO_REGISTRY} \
            --namespace=${OPERATOR_NS} \
            --dry-run="client" -o yaml | ${KUBECTL} ${action} -f -
    fi

    # get user's consent to overwrite the current Kubeturbo CR in the target namespace
    is_cr_exists=$(${KUBECTL} -n ${OPERATOR_NS} get Kubeturbo --field-selector=metadata.name=${KUBETURBO_NAME} -o name)
    if [ -n "${is_cr_exists}" ] && [ "${ACTION}" != "delete" ]; then
        echo "Warning: Kubeturbo CR(${KUBETURBO_NAME}) detected in the namespace(${OPERATOR_NS})!"
        read -p "Please confirm to overwrite the current Kubeturbo CR [Y/n]: " overwriteCR
        [ "${overwriteCR}" = "n" ] || [ "${overwriteCR}" = "N" ] && echo "Installation aborted..." && exit 1
    fi

    cat <<-EOF | ${KUBECTL} ${action} -f -
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
	    repository: ${KUBETURBO_REGISTRY}
	    tag: ${KUBETURBO_VERSION}
	    imagePullSecret: ${private_docker_registry}
	  roleName: ${KUBETURBO_ROLE}
	---
	EOF
    wait_for_deployment ${OPERATOR_NS} ${KUBETURBO_NAME}
}

apply_oauth2_token() {
    if [ ${ACTION} != "delete" ]; then 
        if [ -z "${OAUTH_CLIENT_ID}" ] || [ -z "${OAUTH_CLIENT_SECRET}" ]; then
            echo "Missing OAuth2 client settings, please gather values following the instruction: "
            echo "https://www.ibm.com/docs/en/tarm/latest?topic=cookbook-authenticating-oauth-20-clients-api"
            read -p "Enable enter your OAuth2 client id: " OAUTH_CLIENT_ID
            read -p "Enable enter your OAuth2 client secret: " OAUTH_CLIENT_SECRET
            apply_oauth2_token && return
        fi
    fi

    action="${ACTION}"
    if [ ${ACTION} = "delete" ]; then
        action="${ACTION} --ignore-not-found"
    fi

    cat <<-EOF | ${KUBECTL} ${action} -f -
	apiVersion: v1
	kind: Secret
	metadata:
	  name: turbonomic-credentials
	  namespace: ${OPERATOR_NS}
	type: Opaque
	data:
	  clientid: $(encode_inline ${OAUTH_CLIENT_ID})
	  clientsecret: $(encode_inline ${OAUTH_CLIENT_SECRET})
	---
	EOF
}

setup_tsc() {
    if [ ${ACTION} = "delete" ]; then
        apply_skupper_tunnel
        apply_tsc_cr
        apply_tsc_op
    else
        if [ "${ENABLE_TSC}" != "true" ]; then return; fi
        ${KUBECTL} -n ${OPERATOR_NS} delete secret turbonomic-credentials --ignore-not-found
        apply_tsc_op
        apply_tsc_cr
        apply_skupper_tunnel
        wait_for_tsc_sync_up
    fi
    echo "Successfully ${ACTION} TSC operator in the ${OPERATOR_NS} namespace!"
    ${KUBECTL} -n ${OPERATOR_NS} get role,rolebinding,sa,pod,deploy -l 'app.kubernetes.io/created-by in (t8c-client-operator, turbonomic-t8c-client-operator)'
}

apply_tsc_op() {
    if [ "${TARGET_SUBTYPE}" = "${OCP_TYPE}" ]; then
        apply_tsc_op_subscription
    else
        apply_tsc_op_yaml
    fi
}

apply_tsc_op_subscription() {
    select_cert_op_from_operatorhub "t8c-tsc"
    CERT_TSC_OP_NAME=${CERT_OP_NAME}

    select_cert_op_channel_from_operatorhub ${CERT_TSC_OP_NAME} ${TSC_TARGET_RELEASE}
    CERT_TSC_OP_RELEASE=${CERT_OP_RELEASE}
    CERT_TSC_OP_VERSION=${CERT_OP_VERSION}

    action="${ACTION}"
    if [ ${ACTION} = "delete" ]; then
        action="${ACTION} --ignore-not-found"
    fi

    echo "${ACTION} Certified t8c-tsc operator subscription ..."
    if [ ${ACTION} = "delete" ]; then
        ${KUBECTL} -n ${OPERATOR_NS} ${action} Subscription ${CERT_TSC_OP_NAME}
        ${KUBECTL} -n ${OPERATOR_NS} ${action} csv ${CERT_TSC_OP_VERSION}
        return
    fi
    cat <<-EOF | ${KUBECTL} ${action} -f -
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

apply_tsc_op_yaml() {
    operator_deploy_name="t8c-client-operator-controller-manager"
    operator_service_account="t8c-client-operator-controller-manager"
    
    source_github_repo="https://raw.githubusercontent.com/turbonomic/kubeturbo-deploy"
    operator_yaml_path="deploy/tsc_operator_yamls/operator-bundle.yaml"
    operator_yaml_bundle=$(curl "${source_github_repo}/${OPERATOR_RELEASE}/${operator_yaml_path}" | sed "s/: turbonomic$/: ${OPERATOR_NS}/g" | sed '/^\s*#/d')
    
    apply_operator_bundle "${operator_service_account}" "${operator_deploy_name}" "${operator_yaml_bundle}"
}

apply_tsc_cr() {
    action="${ACTION}"
    if [ ${ACTION} = "delete" ]; then
        action="${ACTION} --ignore-not-found"
    fi

    echo "${ACTION} TSC CR ..."
    tsc_client_name="turbonomicclient-release"
    cat <<-EOF | ${KUBECTL} ${action} -f -
	---
	kind: TurbonomicClient
	apiVersion: clients.turbonomic.ibm.com/v1alpha1
	metadata:
	  name: ${tsc_client_name}
	  namespace: ${OPERATOR_NS}
	spec:
	  global:
	    version: ${KUBETURBO_VERSION}
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

apply_skupper_tunnel() {
    action="${ACTION}"
    if [ ${ACTION} = "delete" ]; then
        action="${ACTION} --ignore-not-found"
    fi

    if [ ${ACTION} = "delete" ]; then 
        echo "${ACTION} secrets for TSC connection ..."
        for it in $(${KUBECTL} get secret -n ${OPERATOR_NS} -l "skupper.io/type" -o name); do
            ${KUBECTL} ${action} -n ${OPERATOR_NS} ${it}
        done
        return
    fi

    if [ -z "${TSC_TOKEN}" ]; then
        if [  -f "${TSC_TOKEN_FILE}" ]; then
            TSC_TOKEN=$(getJsonField "$(cat "${TSC_TOKEN_FILE}")" "tokenData")
        else
            echo "Please follow the wiki to get the TSC token file: "
            echo "https://www.ibm.com/docs/en/tarm/latest?topic=client-secure-deployment-red-hat-openshift-operatorhub#SaaS_OpenShift__OVA_connect__title__1"
            echo "Warning: cannot find TSC token file under: ${TSC_TOKEN_FILE}"
            read -p "Please enter the absolute path for your TSC token: " TSC_TOKEN_FILE
        fi
        apply_skupper_tunnel && return
    fi

    skupper_connection_secret=$(echo "${TSC_TOKEN}" | base64 -d)
    echo "${skupper_connection_secret}" | ${KUBECTL} ${action} -n ${OPERATOR_NS} -f -

    echo "Waiting for setting up TSC connection..."
    retry_count=0
    while true; do
        tunnel_svc=$(${KUBECTL} -n ${OPERATOR_NS} get service --field-selector=metadata.name=remote-nginx-tunnel -o name)
        if [ -n "${tunnel_svc}" ];then break; fi
        retry_count=$((retry_count + 1))
        message=$(retry ${retry_count})
        if [ $? -eq 0 ]; then
            echo "${message}"
        else
            echo "Failed to setup the TSC connection, please request another one from the endpoint or regenerate the script from the UI!" 
            exit 1
        fi
    done
    echo "Skupper connection established!"
}

wait_for_tsc_sync_up() {
    # Wait for CR updates (watch on target server url updates)
    echo "Waiting for Kubeturbo CR updates..."
    retry_count=0
    while true; do
        turbo_server=$(${KUBECTL} -n ${OPERATOR_NS} get Kubeturbos ${KUBETURBO_NAME} -o=jsonpath='{.spec.serverMeta.turboServer}' | grep remote-nginx-tunnel)
        if [ -n "${turbo_server}" ];then break; fi
        retry_count=$((retry_count + 1))
        message=$(retry ${retry_count})
        if [ $? -eq 0 ]; then
            echo "${message}"
        else
            echo "There's no updates from the TSC client, please double-check if the Turbo server can reach out your current cluster!" 
            exit 1
        fi
    done

    # Restart the kubeturbo pod to secure the updates if the operator hasn't restart the pod yet
    kubeturbo_pod=$(${KUBECTL} -n ${OPERATOR_NS} get pods --field-selector=status.phase=Running -o name | grep ${KUBETURBO_NAME})
    if [ -n "${kubeturbo_pod}" ]; then
        ${KUBECTL} -n ${OPERATOR_NS} delete ${kubeturbo_pod} --ignore-not-found
        wait_for_deployment ${OPERATOR_NS} ${KUBETURBO_NAME}
    fi
}

select_cert_op_from_operatorhub() {
    target=$1
    echo "Fetching Openshift certified ${target} operator from OperatorHub ..."
    cert_ops=$(${KUBECTL} get packagemanifests -o jsonpath="{range .items[*]}{.metadata.name} {.status.catalogSource} {.status.catalogSourceNamespace}{'\n'}{end}" | grep -e ${target} | grep -e "${CARALOG_SOURCE}.*${CARALOG_SOURCE_NS}" | awk '{print $1}')
    cert_ops_count=$(echo "${cert_ops}" | wc -l | awk '{print $1}')
    if [ -z "${cert_ops}" ] || [ ${cert_ops_count} -lt 1 ]; then
        echo "There aren't any certified ${target} operator in the Operatorhub, please contact administrator for more information!" && exit 1
    elif [ ${cert_ops_count} -gt 1 ]; then
        PS3="Fetched mutiple certified ${target} operators in the Operatorhub, please select a number to proceed OR type 'exit' to exit: "
        while true; do
            echo "Available options:"
            i=1; echo "${cert_ops}" | while IFS= read -r cert_op; do
                echo "$i) $cert_op"
                i=$((i + 1))
            done
            read -p "$PS3" REPLY
            validateion_result=$(validate_select_input ${cert_ops_count} ${REPLY})
            if [ $? -eq 0 ]; then
                [ "${REPLY}" = 'exit' ] && exit 0
                cert_ops=$(echo "$cert_ops" | awk "NR==$((REPLY))")
                break
            fi
        done
    fi
    CERT_OP_NAME=${cert_ops}
    echo "Using Openshift certified ${target} operator: ${CERT_OP_NAME}"
}

select_cert_op_channel_from_operatorhub() {
    cert_op_name=${1-${CERT_OP_NAME}}
    target_release=${2-${DEFAULT_RELEASE}}
    echo "Fetching Openshift ${cert_op_name} channels from OperatorHub ..."
    channels=$(${KUBECTL} get packagemanifests ${cert_op_name} -o jsonpath="{range .status.channels[*]}{.name}:{.currentCSV}{'\n'}{end}" | grep "${target_release}")
    channel_count=$(echo "${channels}" | wc -l | awk '{print $1}')
    if [ -z "${channels}" ] || [ ${channel_count} -lt 1 ]; then
        echo "There aren't any channel created for ${cert_op_name}, please contact administrator for more information!" && exit 1
    elif [ ${channel_count} -gt 1 ]; then
        PS3="Fetched mutiple releases, please select a number to proceed OR type 'exit' to exit: "
        while true; do
            echo "Available options:"
            i=1; echo "${channels}" | while IFS= read -r channel; do
                echo "$i) $channel"
                i=$((i + 1))
            done
            read -p "$PS3" REPLY
            validateion_result=$(validate_select_input ${channel_count} "${REPLY}")
            if [ $? -eq 0 ]; then
                [ "${REPLY}" = 'exit' ] && exit 0
                channels=$(echo "$channels" | awk "NR==$((REPLY))")
                break
            fi
        done
    fi
    CERT_OP_RELEASE=$(echo ${channels} | awk -F':' '{print $1}')
    CERT_OP_VERSION=$(echo ${channels} | awk -F':' '{print $2}')
    echo "Using Openshift certified ${cert_op_name} ${CERT_OP_RELEASE} channel, version ${CERT_OP_VERSION}"
}

getJsonField() {
    jsonData=$1 && field=$2
    if [ -z "$(echo "${jsonData}" | grep ${field})" ]; then
        echo -e "Unable to get field ${field} due to:\n${jsonData}" >&2 && exit 1
    fi
    echo "${jsonData}" | sed -e "s/^{//g" -e "s/}$//g" -e "s/,/\n/g" -e "s/\"//g" | grep ${field} | sed -e "s/[\" ]//g" | awk -F ':' '{print $2}'
}

validate_select_input() {
    opts_count=$1 && opt=$2
    if [ "${opt}" = "exit" ]; then
        echo "Exiting the program ..." >&2 && exit 0
    elif [ -z "$(echo ${opt} | grep -E '^[1-9][0-9]*$')" ]; then
        echo "ERROR: Input not a number: ${opt}" >&2 && exit 1
    elif [ ${opt} -le 0 ] || [ ${opt} -gt ${opts_count} ]; then
        echo "ERROR: Input out of range [1 - ${opts_count}]: ${opt}" >&2 && exit 1
    fi
}

wait_for_deployment() {
    if [ ${ACTION} = "delete" ]; then return; fi
    namespace=$1 && deploy_name=$2
    
    echo "Waiting for deployment '${deploy_name}' to start..."
    retry_count=0
    while true; do
        full_deploy_name=$(${KUBECTL} -n ${namespace} get deploy -o name | grep ${deploy_name})
        if [ -n "${full_deploy_name}" ]; then
            deploy_status=$(${KUBECTL} -n ${namespace} rollout status ${full_deploy_name} --timeout=5s 2>&1 | grep "successfully")
            if [ -n "${deploy_status}" ]; then
                deploy_name=$(echo "${full_deploy_name}" | awk -F '/' '{print $2}')
                for pod in $(${KUBECTL} -n ${namespace} get pods -o name | grep ${deploy_name}); do
                    ${KUBECTL} -n ${namespace} wait --for=condition=Ready ${pod}
                done
                break
            fi
        fi
        retry_count=$((retry_count + 1))
        message=$(retry ${retry_count})
        if [ $? -eq 0 ]; then
            echo "${message}"
        else
            echo "Please check following events for more information:"
            ${KUBECTL} -n ${namespace} get events --sort-by='.lastTimestamp' | grep ${deploy_name}
            exit 1
        fi
    done
}

retry() {
    attempts=${1:--999}
    if [ ${attempts} -ge ${MAX_RETRY} ]; then
        echo "ERROR: Resource is not ready in ${MAX_RETRY} attempts." >&2 && exit 1
    else
        attempt_str=$([ ${attempts} -ge 0 ] && echo " (${attempts}/${MAX_RETRY})") 
        echo "Resource is not ready, re-attempt after ${RETRY_INTERVAL}s ...${attempt_str}"
        sleep ${RETRY_INTERVAL}
    fi
}

encode_inline() {
    input=$1
    case "$OSTYPE" in
        darwin*)
            echo "${input}" | base64 -b 0
            ;;
        *)
            echo "${input}" | base64 -w 0
            ;;
    esac
}

password_secret_handler() {
    if [ "${PWD_SECRET_ENCODED}" = "true" ]; then 
        echo "$1" | base64 -d
    else
        echo "$1"
    fi
}

apply_operator_bundle() {
    sa_name="$1"
    deploy_name="$2"
    operator_yaml_str="$3"

    tmp_dir=$(mktemp -d)
    # split out yaml from the yaml bundle
    echo "${operator_yaml_str}" | awk '/^---/{i++} {file = "'${tmp_dir}'/yaml_part_" i ".yaml"; print > file}'
    for yaml_part in $(ls ${tmp_dir}); do
        yaml_abs_path="${tmp_dir}/${yaml_part}"
        kind_name_str=$(${KUBECTL} create -f ${yaml_abs_path} --dry-run=client -o=jsonpath="{.kind} {.metadata.name}")
        obj_kind=$(echo ${kind_name_str} | awk '{print $1}')
        obj_name=$(echo ${kind_name_str} | awk '{print $2}')

        is_object_exists=$(${KUBECTL} -n ${OPERATOR_NS} get ${obj_kind} --field-selector=metadata.name=${obj_name} -o name)
        if [ "${ACTION}" = "delete" ]; then
            # delete k8s resources if exists (avoid cluster resources)
            skip_target=$(should_skip_delete_k8s_object "${obj_kind}" ${skip_list})
            [ -n "${is_object_exists}" ] && [ "${skip_target}" = "false" ] && ${KUBECTL} ${ACTION} -f ${yaml_abs_path}
        elif [ -n "${is_object_exists}" ] && [ "${obj_kind}" = "ClusterRoleBinding" ]; then
            # if cluster role binding exists, patch it with target services account
            isClusterRoleBinded=$(${KUBECTL} get ${obj_kind} ${obj_name} -o=jsonpath='{range .subjects[*]}{.namespace}{"\n"}{end}' | grep ${OPERATOR_NS})
            if [ -z "${isClusterRoleBinded}" ]; then 
                ${KUBECTL} patch ${obj_kind} ${obj_name} --type='json' -p='[{"op": "add", "path": "/subjects/-", "value": {"kind": "ServiceAccount", "name": "'${sa_name}'", "namespace": "'${OPERATOR_NS}'"}}]'
            fi
        else
            # either create or update the k8s object
            action=$([ -z "${is_object_exists}" ] && echo "create --save-config" || echo "apply")
            ${KUBECTL} ${action} -f ${yaml_abs_path}
        fi
    done
    rm -rf "${tmp_dir}"

    # check if the operator is ready
    [ "${ACTION}" != "delete" ] && wait_for_deployment ${OPERATOR_NS} ${deploy_name}
}

should_skip_delete_k8s_object() {
    k8s_kind=$1
    [ "${k8s_kind}" = "Namespace" ] && echo "true" && return
    for it in ${K8S_CLUSTER_KINDS[@]}; do
        [ "${it}" = "${k8s_kind}" ] && echo "true" && return
    done
    echo "false"
}

################## MAIN ##################
dependencies_check && validate_args $@ && confirm_installation && main
