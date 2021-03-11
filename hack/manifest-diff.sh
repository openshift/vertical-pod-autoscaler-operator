#!/bin/bash

# This check makes sure that the install manifests for a manual install (in install/deploy/)
# are in sync with the OLM install manifests (in manifests/)

repo_base="$( dirname "$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )")"
repo_name=$(basename "${repo_base}")
cd "${repo_base}"
if ! [ -x bin/json2yaml -a -x bin/yaml2json ]; then
  echo "Missing test utilities bin/json2yaml and/or bin/yaml2json. 'make build-testutil' must be run first"
  exit 1
fi
if [ "$NO_DOCKER" = "1" -o -n "$IS_CONTAINER" ]; then
  exitcode=0
  outdir="$(mktemp --tmpdir -d manifest-diff.XXXXXXXXXX)"
  trap "rm -rf '${outdir}'" EXIT

  # Step 1: Compare RBAC from install/deploy/02_vpa-rbac.yaml with RBAC from $csvfile
  csvfile="$(ls manifests/[0-9].[0-9]/vertical-pod-autoscaler.v[0-9].[0-9].[0-9].clusterserviceversion.yaml | sort -r | head -1)"
  rbacfile="install/deploy/02_vpa-rbac.yaml"
  out1="${outdir}/rbac-from-02_vpa-rbac.yaml"
  out2="${outdir}/rbac-from-$(basename "$csvfile")"
  sed -f hack/yamls2list.sed "$rbacfile" | bin/yaml2json | jq -f hack/filter-rbac.jq | bin/json2yaml > "$out1"
  bin/yaml2json "$csvfile" | jq -f hack/filter-rbac.jq | bin/json2yaml > "$out2"
  if ! diff -q "$out1" "$out2"; then
    echo
    echo "Sorted/normalized $rbacfile:"
    echo
    cat "$out1"
    echo
    echo "Sorted/normalized $csvfile:"
    echo
    cat "$out2"
    echo
    echo diff -u "$out1" "$out2"
    echo
    diff -u "$out1" "$out2"
    echo
    echo "$0 failed. Permissions not equivalent in $rbacfile and $csvfile"
    echo "If changes are made to $rbacfile, equivalent changes should be made to $csvfile (and vice-versa)."
    echo
    exitcode=1
  fi

  # Step 2: Compare the VPA controller CRD in install/deploy/ with the one from manifests/
  crdfile="$(ls manifests/[0-9].[0-9]/vertical-pod-autoscaler-controller.crd.yaml | sort -r | head -1)"
  if ! diff -wu install/deploy/01_vpacontroller.crd.yaml "$crdfile"; then
    echo
    echo "$0 failed. CRDs don't match: install/deploy/01_vpacontroller.crd.yaml and $crdfile"
    echo "If changes are made to install/deploy/01_vpacontroller.crd.yaml, equivalent changes should be made to $crdfile (and vice-versa)."
    echo
    exitcode=1
  fi

  # Step 3: Compare the VPA CRD in install/deploy/ with the one from manifests/
  crdfile="$(ls manifests/[0-9].[0-9]/vpa-v1.crd.yaml | sort -r | head -1)"
  if ! diff -wu install/deploy/05_vpa-crd.yaml "$crdfile"; then
    echo
    echo "$0 failed. CRDs don't match: install/deploy/05_vpa-crd.yaml and $crdfile"
    echo "If changes are made to install/deploy/05_vpa-crd.yaml, equivalent changes should be made to $crdfile (and vice-versa)."
    echo
    exitcode=1
  fi

  # Step 4: Compare the VPA CRD in install/deploy/ with the one from manifests/
  crdfile="$(ls manifests/[0-9].[0-9]/vpacheckpoint-v1.crd.yaml | sort -r | head -1)"
  if ! diff -wu install/deploy/06_vpacheckpoint-crd.yaml "$crdfile"; then
    echo
    echo "$0 failed. CRDs don't match: install/deploy/06_vpacheckpoint-crd.yaml and $crdfile"
    echo "If changes are made to install/deploy/06_vpacheckpoint-crd.yaml, equivalent changes should be made to $crdfile (and vice-versa)."
    echo
    exitcode=1
  fi

  exit $exitcode
else
  podman run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${repo_base}:/go/src/github.com/openshift/${repo_name}:z" \
    --workdir "/go/src/github.com/openshift/${repo_name}" \
    openshift/origin-release:golang-1.15 \
    ./hack/manifest-diff.sh "${@}"
fi;
