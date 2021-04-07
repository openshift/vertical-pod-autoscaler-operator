#!/bin/sh
if [ "$NO_DOCKER" = "1" -o "$IS_CONTAINER" != "" ]; then
  yamllint --config-data "{extends: default, rules: {indentation: {indent-sequences: false}, line-length: {level: warning, max: 120}}}" examples install manifests
else
  podman run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/workdir:z" \
    --entrypoint sh \
    quay.io/coreos/yamllint \
    ./hack/yaml-lint.sh
fi;
