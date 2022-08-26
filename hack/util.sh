#!/usr/bin/env bash

# This library holds common bash variables and utility functions.

# Variables
#
OS="$(go env GOOS)"
ARCH="$(go env GOARCH)"
root_dir="$(cd "$(dirname "$0")/.." ; pwd)"
dest_dir="${root_dir}/bin"
mkdir -p "${dest_dir}"

# kind
kind_version="v0.8.1"
kind_bin_name="kind-${OS}-${ARCH}"
kind_path="${dest_dir}/kind"
#kubectl
kubectl_path="${dest_dir}/kubectl"
#oc
oc_path="${dest_dir}/oc"
#istioctl
istio_version="1.11.8"
istioctl_path="${dest_dir}/istioctl"

# Utility Functions
#
#
# util::command-installed checks if the command from argument 1 is installed.
#
# Globals:
#  None
# Arguments:
#  - 1: command name to check if it is installed in PATH
# Returns:
#  0 if command is installed in PATH
#  1 if the command is NOT installed in PATH
function util::command-installed() {
  command -v "${1}" >/dev/null 2>&1 || return 1
  return 0
}
readonly -f util::command-installed

# util::log echoes the supplied argument with a common header.
#
# Globals:
#  None
# Arguments:
#  - 1: string to echo
# Returns:
#  0
function util::log() {
  echo "##### ${1}..."
}
readonly -f util::log

# util::wait-for-condition blocks until the provided condition becomes true
#
# Globals:
#  None
# Arguments:
#  - 1: message indicating what conditions is being waited for (e.g. 'config to be written')
#  - 2: a string representing an eval'able condition.  When eval'd it should not output
#       anything to stdout or stderr.
#  - 3: optional timeout in seconds.  If not provided, waits forever.
# Returns:
#  1 if the condition is not met before the timeout
function util::wait-for-condition() {
  local msg=$1
  # condition should be a string that can be eval'd.
  local condition=$2
  local timeout=${3:-}

  local start_msg="Waiting for ${msg}"
  local error_msg="[ERROR] Timeout waiting for ${msg}"

  local counter=0
  while ! eval ${condition}; do
    if [[ "${counter}" = "0" ]]; then
      echo -n "${start_msg}"
    fi

    if [[ -z "${timeout}" || "${counter}" -lt "${timeout}" ]]; then
      counter=$((counter + 1))
      if [[ -n "${timeout}" ]]; then
        echo -n '.'
      fi
      sleep 1
    else
      echo -e "\n${error_msg}"
      return 1
    fi
  done

  if [[ "${counter}" != "0" && -n "${timeout}" ]]; then
    echo ' done'
  fi
}
readonly -f util::wait-for-condition

# util::stop-container stops a running docker container
#
# Arguments:
#  - 1: container name for quering docker.
function util::stop-container() {
  local container_name
  container_name=$1
  echo "Stopping container: ${container_name}"
  docker stop "${container_name}"
}

readonly -f util::stop-container


# util::start-container start a stopped docker container
# 
# Arguments:
#  1: container name
function util::start-container() {
  local container_name
  container_name=$1
  echo "Starting container: ${container_name}"
  docker start "${container_name}"
} 

readonly -f util::start-container
