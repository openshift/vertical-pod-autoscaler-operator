#!/bin/bash

set -euo pipefail

GOPATH="$(mktemp -d)"
export GOPATH
echo $GOPATH
AUTOSCALER_PKG="github.com/openshift/kubernetes-autoscaler"
RELEASE_VERSION="release-4.8"
echo "Get the github.com/openshift/kubernetes-autoscaler package!"
# GO111MODULE=off go get -u -d "${AUTOSCALER_PKG}/..."
mkdir -p ${GOPATH}/src/k8s.io
cd ${GOPATH}/src/k8s.io && git clone -b ${RELEASE_VERSION} --single-branch https://${AUTOSCALER_PKG}.git autoscaler

echo "Check the VerticalPodAutoScalerController configurations ..."
recommendationOnly=$(kubectl get VerticalPodAutoScalerController default -n openshift-vertical-pod-autoscaler -oyaml|yq ".spec.recommendationOnly" || false)

SCRIPT_ROOT=${GOPATH}/src/k8s.io/autoscaler/vertical-pod-autoscaler/
if $recommendationOnly
then
  echo "recommendationOnly is enabled. Run the recommender e2e tests in upstream ..."
  pushd ${SCRIPT_ROOT}/e2e
  GO111MODULE=on go test -mod vendor ./v1/*go -v --test.timeout=60m --args --ginkgo.v=true --ginkgo.focus="\[VPA\] \[recommender\]" --ginkgo.skip="doesn't drop lower/upper after recommender's restart" --report-dir=/workspace/_artifacts --disable-log-dump
  V1_RESULT=$?
  popd
  echo "v1 recommender test result:" ${V1_RESULT}

  if [ $V1_RESULT -gt 0 ]; then
    echo "Tests failed"
    exit 1
  fi
else
  echo "recommendationOnly is disabled. Run the full-vpa e2e tests in upstream ..."
  pushd ${SCRIPT_ROOT}/e2e
  GO111MODULE=on go test -mod vendor ./v1/*go -v --test.timeout=60m --args --ginkgo.v=true --ginkgo.focus="\[VPA\] \[full-vpa\]" --report-dir=/workspace/_artifacts --disable-log-dump
  V1_RESULT=$?
  popd
  echo "v1 full-vpa test result:" ${V1_RESULT}

  if [ $V1_RESULT -gt 0 ]; then
    echo "Tests failed"
    exit 1
  fi
fi