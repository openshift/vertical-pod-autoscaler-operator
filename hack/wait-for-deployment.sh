#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

util::await_operator_deployment_create() {
    local kubectl="$1"
    local namespace="$2"
    local name="$3"
    local retries="${4:-50}"
    local output

    command -v ${kubectl} >/dev/null 2>&1 || { echo >&2 "${kubectl} not found, aborting."; exit 1; }

    until [[ "${retries}" -le "0" ]]; do
        output=$(${kubectl} get deployment -n "${namespace}" "${name}" -o jsonpath='{.metadata.name}' 2>/dev/null || echo "waiting for olm to deploy the operator")

        if [ "${output}" = "${name}" ] ; then
            echo "${namespace}/${name} has been created" >&2
            return 0
        fi

        retries=$((retries - 1))
        echo "${output} - remaining attempts: ${retries}" >&2

        sleep 3
    done

    echo "error - olm has not created the deployment yet ${namespace}/${name}" >&2
    return 1
}

KUBECTL=$1
NAMESPACE=$2
DEPLOYMENT_NAME=$3
WAIT_TIME=$4

exitcode=$(util::await_operator_deployment_create "${KUBECTL}" "${NAMESPACE}" "${DEPLOYMENT_NAME}" "${WAIT_TIME}")
exit $exitcode
