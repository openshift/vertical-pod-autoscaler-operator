#!/bin/sh
REPO_NAME=$(basename "${PWD}")
if [ "$NO_DOCKER" = "1" -o "$IS_CONTAINER" != "" ]; then
  go vet "${@}"
else
  podman run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/${REPO_NAME}:z" \
    --workdir "/go/src/github.com/openshift/${REPO_NAME}" \
    registry.ci.openshift.org/openshift/release:rhel-9-release-golang-1.22-openshift-4.17 \
    ./hack/go-vet.sh "${@}"
fi;
