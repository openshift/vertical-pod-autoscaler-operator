#!/bin/sh
REPO_NAME=$(basename "${PWD}")
if [ "$NO_DOCKER" = "1" -o "$IS_CONTAINER" != "" ]; then
  go vet "${@}"
else
  podman run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/${REPO_NAME}:z" \
    --workdir "/go/src/github.com/openshift/${REPO_NAME}" \
    openshift/origin-release:golang-1.15 \
    ./hack/go-vet.sh "${@}"
fi;
