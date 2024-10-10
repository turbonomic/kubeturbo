#!/usr/bin/env bash

NAMESPACE=""
DEPLOYMENT_NAME=""
KUBETURBO="kubeturbo"
CREDENTIAL_SECRET_NAME="turbonomic-credentials"

# Function to check if Kubeturbo has already used the OAuth2 credentials
check_current_status() {
    local secret_name=$1
    local namespace=$2

    # Check if the secret exists or not
    secret_data=$(kubectl get secret "$secret_name" -n "$namespace" 2>/dev/null)
    if [ $? -ne 0 ]; then
        return
    fi

    # Extract the 'clientid' and 'clientsecret' fields, decode them from base64, and trim any whitespace
    id_field=$(kubectl get secret "$secret_name" -n "$namespace" -o jsonpath="{.data.clientid}" | base64 --decode 2>/dev/null)
    secret_field=$(kubectl get secret "$secret_name" -n "$namespace" -o jsonpath="{.data.clientsecret}" | base64 --decode 2>/dev/null)

    # Check if the 'clientid' and 'clientsecret' fields exist and are not empty
    if [[ -n "$id_field" && -n "$secret_field" ]]; then
        echo "Migration to OAuth2 credentials already completed in namespace '$namespace', nothing more to do.... exiting the script."
        exit 0
    fi
}

# Function to check the current status of the current deployment
check_deployment_status() {
    local deploy_name=$1
    local namespace=$2
    
    # Get client's consent to continue with the deployment that is not in 'Ready' state
    local targetReplicas=$(kubectl -n $namespace get deployment $deploy_name -o=jsonpath='{.spec.replicas}')
    local readyReplicas=$(kubectl -n $namespace get deployment $deploy_name -o=jsonpath='{.status.readyReplicas}') && readyReplicas=${readyReplicas:-"0"}
    if [[ "$readyReplicas" != "$targetReplicas" || "$readyReplicas" == "0" ]]; then
        echo "Warning: The selected deployment ($deploy_name) is not in Ready state (${readyReplicas}/${targetReplicas})"
        read -p "Do you want to continue with the current deployment ($deploy_name)? [Y/n]: " continueMigration
        if [[ ! "${continueMigration}" =~ [Yy] ]]; then 
            echo "Abort the migration"
            exit 1
        fi
    fi
}

# Function to search for a deployment in all namespaces
search_deployment_in_all_namespaces() {
    local deployment_name=$1

    # Get the list of all deployments in all namespaces
    deployments=$(kubectl get deployments --all-namespaces -o json)

    # Use jq to filter deployments by name and extract the namespace and name
    matched_deployments=$(echo "$deployments" | jq -r --arg deployment_name "$deployment_name" '
        .items[] | select(.metadata.name | contains($deployment_name)) | 
        select(.metadata.name != "kubeturbo-operator") | 
        "\(.metadata.namespace) \(.metadata.name)"
    ')

    # Check if any deployments were found
    if [ -z "$matched_deployments" ]; then
        echo "No deployments found with name containing '$deployment_name'."
        exit 1
    fi

    # Return the matched deployments
    echo "$matched_deployments"
    return 0
}

# Function to prompt the user to choose a Kubeturbo from the list
choose_deployment() {
    local deployments=$1

    IFS=$'\n' read -rd '' -a deployment_array <<<"$deployments"

    echo "Found the following Kubeturbo deployments in the current cluster:"
    for i in "${!deployment_array[@]}"; do
        ns_name=$(echo "${deployment_array[$i]}" | awk '{print $1}')
        dp_name=$(echo "${deployment_array[$i]}" | awk '{print $2}')
        echo "$((i + 1)). Deployment: $dp_name, Namespace: $ns_name"
    done

    echo "Choose a Kubeturbo deployment by number to update with the OAuth2 credentials."
    read -p "Please enter your choice: " choice

    if [[ $choice -gt 0 && $choice -le ${#deployment_array[@]} ]]; then
        selected_deployment=${deployment_array[$((choice - 1))]}
        NAMESPACE=$(echo "$selected_deployment" | awk '{print $1}')
        DEPLOYMENT_NAME=$(echo "$selected_deployment" | awk '{print $2}')
    else
        echo "Invalid choice."
        exit 1
    fi

    return 0
}

# Function to get the ConfigMap name mounted by kubeturbo deployment
get_configmap_name_from_deployment() {
    local deployment_name=$1
    local namespace=$2

    # Get the deployment in JSON format
    deployment_json=$(kubectl get deployment "$deployment_name" -n "$namespace" -o json)

    # Check if kubectl command was successful
    if [ $? -ne 0 ]; then
        echo "Failed to retrieve deployment '$deployment_name' in namespace '$namespace'."
        exit 1
    fi

    # Extract the ConfigMap names from the deployment's volumes
    configmap_names=$(echo "$deployment_json" | jq -r '.spec.template.spec.volumes[] | select(.configMap) | .configMap.name')

    # Check if any ConfigMap names were found
    if [ -z "$configmap_names" ]; then
        echo "No ConfigMaps found mounted in deployment '$deployment_name'."
        exit 1
    fi

    # echo "ConfigMaps mounted by deployment '$deployment_name':"
    echo "$configmap_names"
    return 0
}

# Function to get the turboServer and user credentials value from the ConfigMap
get_turbo_server_value_from_configmap() {
    local config_map_name=$1
    local namespace=$2

    # Get the ConfigMap in JSON format
    config_map_json=$(kubectl get configmap "$config_map_name" -n "$namespace" -o json)

    # Check if kubectl command was successful
    if [ $? -ne 0 ]; then
        echo "Failed to retrieve ConfigMap '$config_map_name' in namespace '$namespace'."
        exit 1
    fi

    # Extract the value of "turboServer", "opsManagerUserName", "opsManagerPassword"
    turbo_server_value=$(echo "$config_map_json" | jq -r '.data["turbo.config"] | fromjson | .communicationConfig.serverMeta.turboServer')
    opsManagerUserName=$(echo "$config_map_json" | jq -r '.data["turbo.config"] | fromjson | .communicationConfig.restAPIConfig.opsManagerUserName')
    opsManagerPassword=$(echo "$config_map_json" | jq -r '.data["turbo.config"] | fromjson | .communicationConfig.restAPIConfig.opsManagerPassword')
    turboTargetName=$(echo "$config_map_json" | jq -r '.data["turbo.config"] | fromjson | .targetConfig.targetName')

    # Wipe out null values to prevent confusions
    if [[ "$opsManagerUserName" == "null" ]]; then opsManagerUserName=""; fi
    if [[ "$opsManagerPassword" == "null" ]]; then opsManagerPassword=""; fi
    if [[ "$turboTargetName" == "null" ]]; then turboTargetName=""; fi

    # Check if "turboServer" field exists
    if [[ "$turbo_server_value" == "null" ]]; then
        echo "Field 'turboServer' not found in ConfigMap '$config_map_name'."
        exit 1
    fi

     # Check if "turboServer" is pointing to the topology processor endpoint directly (TSC approach)
    if [[ "$turbo_server_value" =~ .*/topology-processor ]]; then
        echo "The selected Kubetubro instance uses a different deployment approach: the turboServer is pointing to $turbo_server_value!"
        echo "Please contact support at https://support.ibm.com for further assistance with the migration."
        echo "Abort the migration."
        exit 1
    fi

    echo $turbo_server_value
    echo $opsManagerUserName
    echo $opsManagerPassword
    echo $turboTargetName
    return 0
}

# Function to log in to turbo server using username and password and save the JSESSIONID
login_to_server() {
    local server_url=$1
    local username=$2
    local password=$3

    # Make the REST API call using curl and save the headers
    response=$(mktemp)
    headers=$(mktemp)
    curl -k -s -X POST "$server_url/api/v3/login?hateoas=true" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        --data-urlencode "username=$username" \
        --data-urlencode "password=$password" \
        -D "$headers" -o "$response"

    # Check if the request was successful
    if [ $? -ne 0 ]; then
        echo "Failed to send the request."
        return 1
    fi

    # Extract the status code from the headers
    status_code=$(head -n 1 "$headers" | awk '{print $2}')

    # Check the status code
    if [[ $status_code -eq 200 ]]; then
        # Extract the JSESSIONID from the response headers
        jsessionid=$(cat "$headers" | grep -i 'Set-Cookie' | grep -o 'JSESSIONID=[^;]*' | cut -d'=' -f2)
    fi

    echo "$status_code"
    echo "$jsessionid"
    # Clean up temporary files
    rm "$response" "$headers"
    return 0
}

# Function to create the oauth2 client using the JSESSIONID
request_oauth2_client_credentials() {
    local server_url=$1
    local jsessionid=$2
    local clientName=${3:-"kubeturbo"}

    # JSON payload
    local payload=$(
        cat <<EOF
	{
	  "clientName": "$clientName",
	  "grantTypes": [
	    "client_credentials"
	  ],
	  "clientAuthenticationMethods": [
	    "client_secret_post"
	  ],
	  "scopes": [
	    "role:PROBE_ADMIN"
	  ],
	  "tokenSettings": {
	    "accessToken": {
	      "ttlSeconds": 600
	    }
	  }
	}
EOF
    )

    # Make the REST API call using curl
    response=$(mktemp)
    headers=$(mktemp)
    curl -k -s -X POST "$server_url/vmturbo/rest/authorization/oauth2/clients" \
        -H "Content-Type: application/json" \
        -H "Cookie: JSESSIONID=$jsessionid" \
        -d "$payload" \
        -D "$headers" -o "$response"

    # Check if the request was successful
    if [ $? -ne 0 ]; then
        echo "Failed to create the OAuth2 client."
        return 1
    fi

    # Extract the status code from the headers
    status_code=$(head -n 1 "$headers" | awk '{print $2}')

    # Check the status code
    if [[ $status_code -eq 200 ]]; then
        clientId=$(cat "$response" | jq -r '.clientId')
        clientSecret=$(cat "$response" | jq -r '.clientSecret')
    fi

    # Parse the response to get the token
    echo "$status_code"
    echo "$clientId"
    echo "$clientSecret"

    # Clean up temporary files
    rm "$response" "$headers"
    return 0
}

# Function to create turbonomic-credentials secret with client id/secret
create_k8s_secret() {
    local namespace=$1
    local clientId=$2
    local clientSecret=$3

    # Check if the secret exists
    secret_exists=$(kubectl get secret "$CREDENTIAL_SECRET_NAME" -n "$namespace" --ignore-not-found)
    if [[ ! -z $secret_exists ]]; then
        echo "Secret '$CREDENTIAL_SECRET_NAME' exists in namespace '$namespace'. Deleting it..."
        kubectl delete secret "$CREDENTIAL_SECRET_NAME" -n "$namespace" >/dev/null 2>&1
    fi

    # Create the secret using kubectl
    kubectl create secret generic $CREDENTIAL_SECRET_NAME \
        --namespace="$namespace" \
        --from-literal="clientid=$clientId" \
        --from-literal="clientsecret=$clientSecret" >/dev/null 2>&1

    # Check if the secret was created successfully
    if [ $? -ne 0 ]; then
        echo "Failed to create the secret."
        exit 1
    fi

    echo "Secret '$CREDENTIAL_SECRET_NAME' created successfully in namespace '$namespace'."
    return 0
}

# Function to check if the turbonomic-credentials secret is mounted by Kubeturbo deployment
check_secret_mounted_by_kubeturbo() {
    local deployment_name=$1
    local namespace=$2
    local secret_name=$3

    # Get the deployment's JSON configuration
    deployment_json=$(kubectl get deployment "$deployment_name" -n "$namespace" -o json)

    # Check if the deployment was fetched successfully
    if [ $? -ne 0 ]; then
        echo "Failed to get the deployment '$deployment_name' in namespace '$namespace'."
        return 1
    fi

    # Check if the secret is mounted as a volume
    secret_volume=$(echo "$deployment_json" | jq -r --arg secret_name "$secret_name" \
        '.spec.template.spec.volumes[]? | select(.secret.secretName == $secret_name) | .name')

    if [ -z "$secret_volume" ]; then
        echo "Secret '$secret_name' is not mounted by deployment '$deployment_name'."
        return 1
    fi

    # Check if the secret is mounted in the containers
    secret_mounted=$(echo "$deployment_json" | jq -r --arg secret_volume "$secret_volume" \
        '.spec.template.spec.containers[]? | select(.volumeMounts[]?.name == $secret_volume) | .name')

    if [ -z "$secret_mounted" ]; then
        echo "Secret '$secret_name' is not mounted by deployment '$deployment_name'."
        return 1
    fi

    echo "Secret '$secret_name' has already been mounted by deployment '$deployment_name'."
    return 0
}

# Function to patch the Kubeturbo deployment with the secret and mount the secret as a volume
patch_deployment_with_secret() {
    local deployment_name=$1
    local namespace=$2
    local secret_name=$3

    # Get the deployment's JSON configuration
    deployment_json=$(kubectl get deployment "$deployment_name" -n "$namespace" -o json)

    # Check if the deployment was fetched successfully
    if [ $? -ne 0 ]; then
        echo "Failed to get the deployment '$deployment_name' in namespace '$namespace'."
        exit 1
    fi

    # Extract the container name from the JSON
    container_name=$(echo "$deployment_json" | jq -r '.spec.template.spec.containers[0].name')

    patch=$(
        cat <<EOF
	{
	    "spec": {
	        "template": {
	            "spec": {
	                "volumes": [
	                    {
	                        "name": "turbonomic-credentials-volume",
	                        "secret": {
	                            "secretName": "$secret_name"
	                        }
	                    }
	                ],
	                "containers": [
	                    {
	                        "name": "$container_name",
	                        "volumeMounts": [
	                            {
	                                "name": "turbonomic-credentials-volume",
	                                "mountPath": "/etc/turbonomic-credentials"
	                            }
	                        ]
	                    }
	                ]
	            }
	        }
	    }
	}
EOF
    )
    kubectl patch deployment "$deployment_name" -n "$namespace" --patch "$patch"

    # Check if the patch was applied successfully
    if [ $? -ne 0 ]; then
        echo "Failed to patch the deployment '$deployment_name'."
        exit 1
    fi

    echo "Deployment '$deployment_name' patched successfully to mount secret '$secret_name'."
    return 0
}

# Function to read username and password from the existing secret
read_user_credentials_from_secret() {

    local secret_name=$1
    local namespace=$2

    # Retrieve the secret data
    secret_data=$(kubectl get secret "$secret_name" -n "$namespace" --ignore-not-found)

    if [[ -z $secret_data ]]; then
        echo "Warning: Secret '$secret_name' not found in namespace '$namespace'"
        return 1
    fi

    # Extract and decode username and password
    username=$(kubectl get secret "$secret_name" -n "$namespace" -o jsonpath="{.data.username}" | base64 --decode 2>/dev/null)
    password=$(kubectl get secret "$secret_name" -n "$namespace" -o jsonpath="{.data.password}" | base64 --decode 2>/dev/null)

    # Check if username or password is missing
    if [[ -z $username || -z $password ]]; then
        echo "Warning: Failed to retrieve 'username' or 'password' from the secret $secret_name or they are missing."
        return 1
    fi

    echo "$username"
    echo "$password"
}

# Function to restart the Kubeturbo pod
restart_kubeturbo_pod() {

    local deployment_name=$1
    local namespace=$2

    # Scale down to 0 replicas
    echo "Scaling down deployment '$deployment_name' in namespace '$namespace' to 0 replicas..."
    kubectl scale deployment "$deployment_name" -n "$namespace" --replicas=0

    if [[ $? -ne 0 ]]; then
        echo "Failed to scale down deployment '$deployment_name'."
        exit 1
    fi

    # Scale back up to 1 replica
    echo "Scaling up deployment '$deployment_name' in namespace '$namespace' to 1 replica..."
    kubectl scale deployment "$deployment_name" -n "$namespace" --replicas=1

    if [[ $? -eq 0 ]]; then
        echo "Deployment '$deployment_name' restarted successfully."
    else
        echo "Failed to scale up deployment '$deployment_name'."
        exit 1
    fi
}

exit_on_subshell_error() {
    [ $? -ne 0 ] && echo "$1" && exit 1
}

main() {
    # Check if jq is installed
    if ! command -v jq &>/dev/null; then
        echo "jq is not installed. Please install jq to use this function."
        exit 1
    fi

    # Search for the deployment in all namespaces
    matched_deployments=$(search_deployment_in_all_namespaces "$KUBETURBO")
    exit_on_subshell_error "$matched_deployments"

    # Prompt the user to choose a deployment
    choose_deployment "$matched_deployments"

    echo "You selected deployment '$DEPLOYMENT_NAME' in namespace '$NAMESPACE' for migration."
    
    # Confirm status of the current deployment
    check_deployment_status $DEPLOYMENT_NAME $NAMESPACE

    # Check if the migration has completed, if it has, exit the script
    check_current_status $CREDENTIAL_SECRET_NAME $NAMESPACE

    # Get server and user info from the configMap
    kubeturbo_cm=$(get_configmap_name_from_deployment $DEPLOYMENT_NAME $NAMESPACE)
    exit_on_subshell_error "$kubeturbo_cm"
    server_info=$(get_turbo_server_value_from_configmap $kubeturbo_cm $NAMESPACE)
    exit_on_subshell_error "$server_info"
    server_addr=$(echo "$server_info" | sed -n '1p')
    user_name=$(echo "$server_info" | sed -n '2p')
    user_password=$(echo "$server_info" | sed -n '3p')
    target_name=$(echo "$server_info" | sed -n '4p')

    # Update the username and password if it's defined in the secret
    user_credentials=$(read_user_credentials_from_secret $CREDENTIAL_SECRET_NAME $NAMESPACE)
    if [ $? -eq 0 ]; then
        user_name=$(echo "$user_credentials" | sed -n '1p')
        user_password=$(echo "$user_credentials" | sed -n '2p')
        echo "Switch to use the username and password defined in the exsiting secret $CREDENTIAL_SECRET_NAME"
    else
        echo "$user_credentials"
    fi

    # Abort if username and password set is not complete, might under different deployment apprach
    if [[ -z "$user_name" || -z "$user_password" ]]; then
        echo "The selected Kubetubro instance might uses a different deployment approach: incomplete username and password set detected!"
        echo "Please contact support at https://support.ibm.com for further assistance with the migration."
        echo "Abort the migration."
        exit 1
    fi

    # Log in the server
    login_response=$(login_to_server $server_addr $user_name $user_password)
    exit_on_subshell_error "$login_response"
    response_code=$(echo "$login_response" | sed -n '1p')
    jsessionid=$(echo "$login_response" | sed -n '2p')
    if [[ "$response_code" != "200" ]]; then
        echo "Failed to log in to the server $server_addr with the response code '$response_code'."
        echo "Please contact support at https://support.ibm.com for further assistance with the migration."
        echo "Abort the migration."
        exit 1
    fi

    # Create the OAuth2 client credentials
    client_credentials=$(request_oauth2_client_credentials $server_addr $jsessionid $target_name)
    exit_on_subshell_error "$client_credentials"
    response_code=$(echo "$client_credentials" | sed -n '1p')
    client_id=$(echo "$client_credentials" | sed -n '2p')
    client_secret=$(echo "$client_credentials" | sed -n '3p')
    if [[ "$response_code" != "200" ]]; then
        echo "Failed to create the OAuth2 client with the response code $response_code from the server"
        echo "Please verify that OAuth2 is enabled on the Turbonomic Server and try running the script again"
        echo "Abort the migration."
        exit 1
    fi

    # Create the secret
    create_k8s_secret $NAMESPACE $client_id $client_secret

    # Check if the secret is mounted by the kubeturbo
    check_secret_mounted_by_kubeturbo "$DEPLOYMENT_NAME" "$NAMESPACE" "$CREDENTIAL_SECRET_NAME"
    if [ $? -ne 0 ]; then
        # If the secret is NOT mounted, patch the deployment
        patch_deployment_with_secret "$DEPLOYMENT_NAME" "$NAMESPACE" "$CREDENTIAL_SECRET_NAME"
    else
        # If the secret is mounted, restart kubeturbo pod to load the updated secret
        restart_kubeturbo_pod "$DEPLOYMENT_NAME" "$NAMESPACE"
    fi

    echo "Migration is done. Kubeturbo should be running with OAuth2 credentials."

}

# Execute the main function
main
