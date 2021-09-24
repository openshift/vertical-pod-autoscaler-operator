#!/bin/bash

set -e

# This shell script substitues the operator and operand image urls in the specified
# ClusterServiceVersion file with the desired values.
# It depends on the following ENV vars:
#
# CSV_FILE_PATH_IN_REGISTRY_IMAGE: where the CSV file is located within the operator registry image.
# OLD_OPERATOR_IMAGE_URL_IN_CSV: operator imgae url in the csv to be substituted.
# OPERATOR_IMAGE_URL: new operator image url
#
# OLD_OPERAND_IMAGE_URL_IN_CSV: operand imgae url in the csv to be substituted.
# OPERAND_IMAGE_URL: new operand image url

echo "dumping ENV vars"
echo "CSV_FILE_PATH_IN_REGISTRY_IMAGE=${CSV_FILE_PATH_IN_REGISTRY_IMAGE}"
echo "OLD_OPERATOR_IMAGE_URL_IN_CSV=${OLD_OPERATOR_IMAGE_URL_IN_CSV}"
echo "OPERATOR_IMAGE_URL=${OPERATOR_IMAGE_URL}"
echo "OLD_OPERAND_IMAGE_URL_IN_CSV=${OLD_OPERAND_IMAGE_URL_IN_CSV}"
echo "OPERAND_IMAGE_URL=${OPERAND_IMAGE_URL}"

sed "s,${OLD_OPERATOR_IMAGE_URL_IN_CSV},${OPERATOR_IMAGE_URL},g" -i "${CSV_FILE_PATH_IN_REGISTRY_IMAGE}"
sed "s,${OLD_OPERAND_IMAGE_URL_IN_CSV},${OPERAND_IMAGE_URL},g" -i "${CSV_FILE_PATH_IN_REGISTRY_IMAGE}"

echo "substitution complete"

grep -C 2 -F "${OPERATOR_IMAGE_URL}" < ${CSV_FILE_PATH_IN_REGISTRY_IMAGE} || { echo "Substitution failed: sed 's,${OLD_OPERATOR_IMAGE_URL_IN_CSV},${OPERATOR_IMAGE_URL},g' -i '${CSV_FILE_PATH_IN_REGISTRY_IMAGE}'"; exit 1; }
grep -C 2 -F "${OPERAND_IMAGE_URL}" < ${CSV_FILE_PATH_IN_REGISTRY_IMAGE} || { echo "Substitution failed: sed 's,${OLD_OPERAND_IMAGE_URL_IN_CSV},${OPERAND_IMAGE_URL},g' -i '${CSV_FILE_PATH_IN_REGISTRY_IMAGE}'"; exit 1; }

echo "generating sqlite database"

/usr/bin/initializer --manifests=/manifests --output=/bundle/bundles.db --permissive=true

