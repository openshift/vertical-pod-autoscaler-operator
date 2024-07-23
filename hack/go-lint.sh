#!/bin/sh
# Example:  ./hack/go-lint.sh installer/... pkg/... tests/smoke

REPO_NAME=$(basename "${PWD}")
if [ "$NO_DOCKER" = "1" -o "$IS_CONTAINER" != "" ]; then
  golint -set_exit_status "${@}"
else
  podman run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/${REPO_NAME}:z" \
    --workdir "/go/src/github.com/openshift/${REPO_NAME}" \
    registry.ci.openshift.org/openshift/release:rhel-9-release-golang-1.22-openshift-4.17 \
    ./hack/go-lint.sh "${@}"
fi
