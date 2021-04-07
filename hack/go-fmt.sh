#!/bin/sh

REPO_NAME=$(basename "${PWD}")

if ! [ -x "$(command -v docker)" ]; then
  DOCKER_RUNTIME=podman
else
  DOCKER_RUNTIME=docker
fi

if [ "$NO_DOCKER" = "1" -o "$IS_CONTAINER" != "" ]; then
  for TARGET in "${@}"; do
    find "${TARGET}" -name '*.go' ! -path '*/vendor/*' ! -path '*/.build/*' -exec gofmt -s -w {} \+
  done
  git diff --exit-code
else
  ${DOCKER_RUNTIME} run -it --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/${REPO_NAME}:z" \
    --workdir "/go/src/github.com/openshift/${REPO_NAME}" \
    openshift/origin-release:golang-1.15 \
    ./hack/go-fmt.sh "${@}"
fi
