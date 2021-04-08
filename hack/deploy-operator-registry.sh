#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

NAMESPACE=$1
if [ "${NAMESPACE}" == "" ]; then
  echo "Must specify a namespace" >&2
  exit 1
fi

KUBECTL=$2
if [ "${KUBECTL}" == "" ]; then
  echo "Must specify a path to kubectl/oc binary" >&2
  exit 1
fi

MANIFESTS=$3
if [ "${MANIFESTS}" == "" ]; then
  echo "Must specify a path to the kube manifests" >&2
  exit 1
fi

YAML2JSON=$4
if [ "${YAML2JSON}" == "" -a -x "${YAML2JSON}" ]; then
  echo "Must specify a path to yaml2json" >&2
  exit 1
fi

echo "setting up namespace" >&2
NAME=$(${KUBECTL} get namespace ${NAMESPACE} -o jsonpath='{.metadata.name}' 2>/dev/null || echo "does not exist")
if [ "${NAME}" != "${NAMESPACE}" ]; then
  ${KUBECTL} create ns ${NAMESPACE}
fi

echo "applying configmap" >&2
CONFIGMAP_FILE="${MANIFESTS}/registry-env.yaml"
${KUBECTL} apply -n ${NAMESPACE} -f ${CONFIGMAP_FILE}

echo "creating deployment" >&2
OPERATOR_REGISTRY_DEPLOYMENT_FILE="${MANIFESTS}/registry-deployment.yaml"

OPERATOR_REGISTRY_DEPLOYMENT_NAME=$(${YAML2JSON} ${OPERATOR_REGISTRY_DEPLOYMENT_FILE} | jq -r '.metadata.name')
if [ "${OPERATOR_REGISTRY_DEPLOYMENT_NAME}" == "null" ] || [ "${OPERATOR_REGISTRY_DEPLOYMENT_NAME}" == "" ]; then
  echo "could not retrieve Deployment name from ${OPERATOR_REGISTRY_DEPLOYMENT_FILE}" >&2
  exit 1
fi

${KUBECTL} apply -n ${NAMESPACE} -f ${OPERATOR_REGISTRY_DEPLOYMENT_FILE}

echo "waiting for deployment to be available ${NAMESPACE}/${OPERATOR_REGISTRY_DEPLOYMENT_NAME}" >&2
${KUBECTL} -n ${NAMESPACE} rollout status -w deployment/${OPERATOR_REGISTRY_DEPLOYMENT_NAME}

echo "creating service" >&2
SERVICE_FILE="${MANIFESTS}/service.yaml"
SERVICE_NAME=$(${YAML2JSON} ${SERVICE_FILE} | jq -r '.metadata.name')
if [ "${SERVICE_NAME}" == "null" ] || [ "${SERVICE_NAME}" == "" ]; then
  echo "could not retrieve Service name from ${SERVICE_FILE}" >&2
  exit 1
fi

${KUBECTL} apply -n ${NAMESPACE} -f ${MANIFESTS}/service.yaml

CLUSTER_IP=$(${KUBECTL} -n ${NAMESPACE} get service ${SERVICE_NAME} -o jsonpath='{.spec.clusterIP}' || echo "")
if [ "${CLUSTER_IP}" == "" ]; then
  echo "could not retrieve clusterIP from Service ${SERVICE_NAME}" >&2
  exit 1
fi

echo "clusterIP=${CLUSTER_IP} from Service=${SERVICE_NAME}" >&2

CATALOG_SOURCE_FILE="${MANIFESTS}/catalog-source.yaml"
sed "s/CLUSTER_IP/${CLUSTER_IP}/g" -i "${CATALOG_SOURCE_FILE}"
${KUBECTL} apply -n ${NAMESPACE} -f ${CATALOG_SOURCE_FILE}


