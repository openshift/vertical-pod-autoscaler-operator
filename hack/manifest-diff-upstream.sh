#!/bin/bash

# This check makes sure that the install manifests for a manual install (in install/deploy/)
# are in sync with the upstream install manifests. It should detect when upstream manifests
# have been modified for bug fixes, new features, etc. so that the OpenShift VPA operator
# can likewise be updated so that the VPA code and manifests stay in sync.

# Requires: hack/yamls2list.sed, hack/filter-upstream-rbac.jq, bin/json2yaml, bin/yaml2json
operand_branch="release-4.22"
repo_base="$( dirname "$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )")"
repo_name=$(basename "${repo_base}")
upstream_manifest_url_prefix="https://raw.githubusercontent.com/openshift/kubernetes-autoscaler/$operand_branch/vertical-pod-autoscaler/deploy"

cd "${repo_base}"
if ! [ -x bin/json2yaml -a -x bin/yaml2json ]; then
  echo "Missing test utilities bin/json2yaml and/or bin/yaml2json. 'make build-testutil' must be run first"
  exit 1
fi
exitcode=0
outdir="$(mktemp --tmpdir -d manifest-diff.XXXXXXXXXX)"
trap "rm -rf '${outdir}'" EXIT
mkdir "${outdir}/upstream/"

# Step 1: Compare RBAC from config/vpa/vpa-rbac.yaml with upstream RBAC
upstream_filename="vpa-rbac.yaml"
upstream_file="${outdir}/upstream/${upstream_filename}"
if ! curl -s "$upstream_manifest_url_prefix/$upstream_filename" > "$upstream_file"; then
  exitcode=$?
  echo "Failed to get $upstream_manifest_url_prefix/$upstream_filename"
fi
rbacfile="config/vpa/vpa-rbac.yaml"
out1="${outdir}/rbac-from-upstream-$(basename "$upstream_file")"
out2="${outdir}/rbac-from-$(basename "$rbacfile")"

sed -f hack/yamls2list.sed < "$upstream_file" | bin/yaml2json | jq -f hack/filter-upstream-rbac.jq | bin/json2yaml > "$out1"
# RBAC items related to the OpenShift VPA operator should be removed prior to comparison
sed -f hack/yamls2list.sed < "$rbacfile" | bin/yaml2json | jq 'del(.items[] | select(.metadata.name == "vertical-pod-autoscaler-operator"))' | bin/json2yaml > "$out2"

if ! diff -q "$out1" "$out2"; then
  echo
  echo "Normalized $upstream_file:"
  echo
  cat "$out1"
  echo
  echo "Normalized $rbacfile:"
  echo
  cat "$out2"
  echo
  echo diff -u "$out1" "$out2"
  echo
  diff -u "$out1" "$out2"
  echo
  echo "$0 failed. Permissions not equivalent in $rbacfile and $upstream_file"
  echo "If changes are made to $upstream_file, equivalent changes should be made to $rbacfile"
  echo "If OpenShift-specific changes are made to $rbacfile, those changes should be represented in hack/filter-upstream-rbac.jq so that a transformed upstream rbac will match the downstream changed version."
  echo
  exitcode=1
fi

# Step 2: Compare the VPA CRD in config/vpa/ with the one from manifests/
upstream_filename="vpa-v1-crd-gen.yaml"
upstream_file="${outdir}/upstream/${upstream_filename}"
if ! curl -s "$upstream_manifest_url_prefix/$upstream_filename" >> "$upstream_file"; then
    exitcode=$?
    echo "Failed to get $upstream_manifest_url_prefix/$upstream_filename"
fi
crdfile="config/vpa/vpa-crd.yaml"
out1="${outdir}/crd-from-upstream-$(basename "$upstream_file")"
out2="${outdir}/crd-from-$(basename "$crdfile")"

sed -f hack/yamls2list.sed < "$upstream_file" | bin/yaml2json | jq '.items[] | select(.kind=="CustomResourceDefinition" and .metadata.name=="verticalpodautoscalers.autoscaling.k8s.io")' | bin/json2yaml > "$out1"
bin/yaml2json < "$crdfile" | bin/json2yaml > "$out2"
if ! diff -q "$out1" "$out2"; then
  echo
  echo "Normalized $upstream_file:"
  echo
  cat "$out1"
  echo
  echo "Normalized $crdfile:"
  echo
  cat "$out2"
  echo
  echo diff -u "$out1" "$out2"
  echo
  diff -u "$out1" "$out2"
  echo
  echo "$0 failed. CRDs don't match: $crdfile and $upstream_manifest_url_prefix/$upstream_filename"
  echo "If changes are made to the upstream CRD, equivalent changes should be made to $crdfile."
  echo
  exitcode=1
fi

# Step 3: Compare the VPA Checkpoint CRD in config/vpa/ with the one from manifests/
crdfile="config/vpa/vpacheckpoint-crd.yaml"
out2="${outdir}/crd-from-$(basename "$crdfile")"

sed -f hack/yamls2list.sed < "$upstream_file" | bin/yaml2json | jq '.items[] | select(.kind=="CustomResourceDefinition" and .metadata.name=="verticalpodautoscalercheckpoints.autoscaling.k8s.io")' | bin/json2yaml > "$out1"
bin/yaml2json < "$crdfile" | bin/json2yaml > "$out2"
if ! diff -q "$out1" "$out2"; then
  echo
  echo "Normalized $upstream_file:"
  echo
  cat "$out1"
  echo
  echo "Normalized $crdfile:"
  echo
  cat "$out2"
  echo
  echo diff -u "$out1" "$out2"
  echo
  diff -u "$out1" "$out2"
  echo
  echo "$0 failed. CRDs don't match: $crdfile and $upstream_manifest_url_prefix/$upstream_filename"
  echo "If changes are made to the upstream CRD, equivalent changes should be made to $crdfile."
  echo
  exitcode=1
fi
exit $exitcode
