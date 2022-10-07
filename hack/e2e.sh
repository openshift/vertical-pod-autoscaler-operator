#!/bin/bash

set -euo pipefail

KUBECTL=$1
echo ${KUBECTL}

function run_upstream_vpa_tests() {
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
}

function await_for_controllers() {
  local retries=${1:-10}
  local expected=${2:-}
  while [ ${retries} -ge 0 ]; do
    recommenderReplicas=$(${KUBECTL} get deployment vpa-recommender-default -n openshift-vertical-pod-autoscaler -o jsonpath={.status.replicas})
    recommenderReplicas=${recommenderReplicas:=0}
    
    admissionpluginReplicas=$(${KUBECTL} get deployment vpa-admission-plugin-default -n openshift-vertical-pod-autoscaler -o jsonpath={.status.replicas})
    admissionpluginReplicas=${admissionpluginReplicas:=0}

    updaterReplicas=$(${KUBECTL} get deployment vpa-updater-default -n openshift-vertical-pod-autoscaler -o jsonpath={.status.replicas})
    updaterReplicas=${updaterReplicas:=0}

    if ((${recommenderReplicas} >= 1)) && ((${admissionpluginReplicas} >= 1)) && ((${updaterReplicas} >= 1)) && [ ${expected:=all} = all -o "$retries" -eq 0 ];
    then
      echo "all"
      return
    elif ((${recommenderReplicas} >= 1)) && ((${admissionpluginReplicas} == 0)) && ((${updaterReplicas} == 0)) && [ ${expected:=recommender} = recommender -o "$retries" -eq 0 ];
    then
      echo "recommender"
      return
    fi
    retries=$((retries - 1))
    sleep 5
  done
  echo "unknown"
  return
}

GOPATH="$(mktemp -d)"
export GOPATH
echo $GOPATH
AUTOSCALER_PKG="github.com/openshift/kubernetes-autoscaler"
RELEASE_VERSION="release-4.12"
echo "Get the github.com/openshift/kubernetes-autoscaler package!"
# GO111MODULE=off go get -u -d "${AUTOSCALER_PKG}/..."
mkdir -p ${GOPATH}/src/k8s.io
cd ${GOPATH}/src/k8s.io && git clone -b ${RELEASE_VERSION} --single-branch https://${AUTOSCALER_PKG}.git autoscaler

echo "Check the VerticalPodAutoScalerController configurations ..."
SCRIPT_ROOT=${GOPATH}/src/k8s.io/autoscaler/vertical-pod-autoscaler/

WAIT_TIME=50
curstatus=$(await_for_controllers "$WAIT_TIME")
if [[ "$curstatus" == "all" ]];
then
  echo "All controllers are running"
elif [[ "$curstatus" == "recommender" ]];
then
  echo "Only recommender is running!"
else
  echo "Controllers are not ready!"
  echo "Current deployments and pods:"
  ${KUBECTL} get deployments -n openshift-vertical-pod-autoscaler
  ${KUBECTL} get pods -n openshift-vertical-pod-autoscaler
  ${KUBECTL} get deployments -n openshift-vertical-pod-autoscaler -o yaml
  ${KUBECTL} get pods -n openshift-vertical-pod-autoscaler -o yaml
  exit 1
fi


echo "Setting the default verticalpodautoscalercontroller with {\"spec\":{\"recommendationOnly\": true}}"
${KUBECTL} patch verticalpodautoscalercontroller default -n openshift-vertical-pod-autoscaler --type merge --patch '{"spec":{"recommendationOnly": true}}'
curstatus=$(await_for_controllers "$WAIT_TIME" "recommender")
if [[ "$curstatus" == "recommender" ]];
then
  echo "Only recommender is running!"
else
  echo "error - only recommender should be running!"
  exit 1
fi

recommendationOnly=$(${KUBECTL} get VerticalPodAutoScalerController default -n openshift-vertical-pod-autoscaler -o jsonpath={.spec.recommendationOnly})
recommendationOnly=${recommendationOnly:=false}
## Uncomment to enable the upstream recommender only tests. 
## Disable it because full-vpa tests already covers it and it takes too long to finish these tests that may last longer than the CI cluster's lifespan.
# run_upstream_vpa_tests

${KUBECTL} patch verticalpodautoscalercontroller default -n openshift-vertical-pod-autoscaler --type merge --patch '{"spec":{"recommendationOnly": false}}'
curstatus=$(await_for_controllers "$WAIT_TIME" "all")
if [[ "$curstatus" == "all" ]];
then
  echo "All controllers are running"
else
  echo "error - not all controllers are running! Expected 'all' from await_for_controllers, got '$curstatus' instead"
  echo "\$ ${KUBECTL} get deployment -n openshift-vertical-pod-autoscaler"
  ${KUBECTL} get deployment -n openshift-vertical-pod-autoscaler
  exit 1
fi
recommendationOnly=$(${KUBECTL} get VerticalPodAutoScalerController default -n openshift-vertical-pod-autoscaler -o jsonpath={.spec.recommendationOnly})
recommendationOnly=${recommendationOnly:=false}
run_upstream_vpa_tests
