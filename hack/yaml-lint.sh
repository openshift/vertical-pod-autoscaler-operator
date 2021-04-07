#!/bin/sh
if [ "$NO_DOCKER" = "1" -o "$IS_CONTAINER" != "" ]; then
  yamllint --config-data "{extends: default, rules: {line-length: {level: warning, max: 120}}}" ./examples/
else
  podman run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/workdir:z" \
    --entrypoint sh \
    quay.io/coreos/yamllint \
    ./hack/yaml-lint.sh
fi;
