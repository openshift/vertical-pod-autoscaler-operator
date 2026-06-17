#!/usr/bin/env bash
#
# Downloads OpenShift CRDs needed for envtest from the openshift/api repository.
# The commit is derived from the github.com/openshift/api version in go.mod.
#
# The downloaded CRDs have x-kubernetes-validations stripped because envtest's
# kubebuilder API server may not support the latest CEL validation functions.
#
# Usage: hack/fetch-test-crds.sh
#
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${repo_root}/test/testdata/crd"
yq="${repo_root}/bin/yq"

if [[ ! -x "${yq}" ]]; then
  echo "ERROR: yq not found at ${yq}; run 'make yq' first" >&2
  exit 1
fi

# Extract the commit hash from the openshift/api pseudo-version in go.mod.
# Pseudo-versions look like: v0.0.0-20260610004746-5ce2c3071851
openshift_api_version=$(go list -m -f '{{.Version}}' github.com/openshift/api)
commit=$(echo "${openshift_api_version}" | sed -E 's/.*-([0-9a-f]{12,})$/\1/')

if [[ -z "${commit}" ]]; then
  echo "ERROR: could not extract commit hash from openshift/api version: ${openshift_api_version}" >&2
  exit 1
fi

echo "Using openshift/api commit: ${commit}"

# CRDs to download, as paths relative to the openshift/api repo root.
# We use the TechPreviewNoUpgrade variant to include fields behind feature gates
# (e.g. TLSAdherence) that tests need to exercise.
crds=(
  "config/v1/zz_generated.crd-manifests/0000_10_config-operator_01_apiservers-TechPreviewNoUpgrade.crd.yaml"
)

mkdir -p "${output_dir}"

for crd_path in "${crds[@]}"; do
  filename=$(basename "${crd_path}")
  url="https://raw.githubusercontent.com/openshift/api/${commit}/${crd_path}"
  echo "Downloading ${filename} ..."
  curl -sfL "${url}" \
    | "${yq}" --indent 2 'del(.. | .x-kubernetes-validations?)' \
    > "${output_dir}/${filename}"
done

echo "CRDs saved to ${output_dir}"
