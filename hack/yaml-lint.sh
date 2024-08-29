#!/bin/sh
if [ "$NO_DOCKER" = "1" ] || [ "$IS_CONTAINER" != "" ]; then
  echo "Running yamllint version: $(yamllint --version)..."
  yamllint -f colored .
else
  podman run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/data:z" \
    --entrypoint sh \
    docker.io/cytopia/yamllint:alpine \
    ./hack/yaml-lint.sh
fi

# image from https://hub.docker.com/r/cytopia/yamllint
# repository seems to be dormant now, so latest is version yamllint 1.32.0 until further notice
