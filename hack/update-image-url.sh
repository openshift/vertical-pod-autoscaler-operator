#!/bin/bash

set -ex

which jq &>/dev/null || { echo "Please install jq (https://stedolan.github.io/jq/)."; exit 1; }

CONFIGMAP_ENV_FILE=$1
if [ "${CONFIGMAP_ENV_FILE}" == "" ]; then
  echo "Must specify path to the ConfigMap env file"
  exit 1
fi

FILE_TO_CHANGE=$2
if [ "${FILE_TO_CHANGE}" == "" ]; then
  echo "Must specify a path to the file that you want to update"
  exit 1
fi

REGISTRY_IMAGE=$(cat ${CONFIGMAP_ENV_FILE} | jq '.data.OPERATOR_REGISTRY_IMAGE_URL')
OPERATOR_IMAGE=$(cat ${CONFIGMAP_ENV_FILE} | jq '.data.OPERATOR_IMAGE_URL')
OPERAND_IMAGE=$(cat ${CONFIGMAP_ENV_FILE} | jq '.data.OPERAND_IMAGE_URL')

sed "s,VPA_OPERATOR_REGISTRY_IMAGE,${REGISTRY_IMAGE},g" -i "${FILE_TO_CHANGE}"
sed "s,VPA_OPERATOR_IMAGE,${OPERATOR_IMAGE},g" -i "${FILE_TO_CHANGE}"
sed "s,VPA_OPERAND_IMAGE,${OPERAND_IMAGE},g" -i "${FILE_TO_CHANGE}"
