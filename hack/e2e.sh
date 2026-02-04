#!/bin/bash

set -euo pipefail

function print_help {
  echo "Usage: $0 <kubectl> [suite]"
  echo ""
  echo "Arguments:"
  echo "  kubectl    Path to kubectl binary (required)"
  echo "  suite      Test suite to run (optional, default: full-vpa)"
  echo ""
  echo "Available test suites:"
  echo "  - recommender          Test VPA recommender component"
  echo "  - updater              Test VPA updater component"
  echo "  - admission-controller Test VPA admission controller component"
  echo "  - actuation            Test VPA actuation"
  echo "  - full-vpa             Test full VPA stack (default)"
}

namespace="openshift-vertical-pod-autoscaler"
components=(vpa-recommender-default vpa-admission-plugin-default vpa-updater-default)

if [ $# -lt 1 ] || [ "$1" == "-h" ] || [ "$1" == "--help" ]; then
  print_help
  exit 1
fi

KUBECTL=$1
SUITE=${2:-full-vpa}

case ${SUITE} in
  recommender|updater|admission-controller|actuation|full-vpa)
    ;;
  *)
    echo "ERROR: Invalid suite '${SUITE}'"
    echo ""
    print_help
    exit 1
    ;;
esac

# If running in OpenShift CI, we have an artifact dir, otherwise use /tmp
REPORT_DIR="${ARTIFACT_DIR:-/tmp/workdir}"

echo "Using kubectl: ${KUBECTL}"
echo "Test suite: ${SUITE}"

function cleanup() {
  if [ "${SUITE}" == "recommender" ]; then
    ${KUBECTL} patch verticalpodautoscalercontroller default -n "${namespace}" --type merge --patch '{"spec":{"recommendationOnly": false}}'
  elif [ "${SUITE}" != "full-vpa" ]; then
    ${KUBECTL} scale --replicas=1 deployment/vertical-pod-autoscaler-operator -n "${namespace}"
  fi
}

function run_upstream_vpa_tests() {
  echo "Running ${SUITE} e2e tests from upstream..."
  pushd "${SCRIPT_ROOT}/e2e" > /dev/null
  
  VPA_NAMESPACE="${namespace}" GO111MODULE=on go test -mod vendor ./v1/*go -v \
    --test.timeout=125m \
    --args \
    --ginkgo.v=true \
    --ginkgo.timeout=2h \
    --ginkgo.focus="\[VPA\] \[${SUITE}\]" \
    --report-dir="${REPORT_DIR}/vpa_artifacts" \
    --disable-log-dump \
    --allowed-not-ready-nodes=3
  
  local result=$?
  popd > /dev/null

  return $result
}

# Returns expected replica count for a controller in the current test suite
# Returns: 1 if needed, 0 if not needed
function expected_replicas() {
  local controller=$1
  
  case ${SUITE} in
    full-vpa)
      echo 1  # All controllers needed
      ;;
    recommender)
      [ "${controller}" == "vpa-recommender-default" ] && echo 1 || echo 0
      ;;
    updater)
      [ "${controller}" == "vpa-updater-default" ] && echo 1 || echo 0
      ;;
    admission-controller)
      [ "${controller}" == "vpa-admission-plugin-default" ] && echo 1 || echo 0
      ;;
    actuation)
      if [ "${controller}" == "vpa-updater-default" ] || [ "${controller}" == "vpa-admission-plugin-default" ]; then
        echo 1
      else
        echo 0
      fi
      ;;
  esac
}

# TODO(maxcao13): This is a bad hack for non-recommender suites, but we need to "turn off" other controllers when running a specific component suite.
# For example, the recommender can interfere with a VerticalPodAutoscaler object that we created during the admission controller suite.
function disable_controllers() {
  # If suite is full-vpa, all controllers should run
  if [ "${SUITE}" == "full-vpa" ]; then
    return 0
  fi

  trap cleanup EXIT
  if [ "${SUITE}" == "recommender" ]; then
    # Test the recommendationOnly mode feature, which only enables the recommender controller
    ${KUBECTL} patch verticalpodautoscalercontroller default -n "${namespace}" --type merge --patch '{"spec":{"recommendationOnly": true}}'
  else
    # Scale down the operator for non-recommender suites
    ${KUBECTL} scale --replicas=0 deployment/vertical-pod-autoscaler-operator -n "${namespace}"

    for controller in "${components[@]}"; do
      local expected
      expected=$(expected_replicas "${controller}")
      if [ "${expected}" -eq 0 ]; then
        ${KUBECTL} scale --replicas=0 deployment/"${controller}" -n "${namespace}"
      fi
    done
  fi
}

function wait_for_controllers() {
  local retries=${1:-10}
  local recommender_expected
  local admission_expected
  local updater_expected
  recommender_expected=$(expected_replicas "vpa-recommender-default")
  admission_expected=$(expected_replicas "vpa-admission-plugin-default")
  updater_expected=$(expected_replicas "vpa-updater-default")

  echo "Waiting for VPA controllers to be ready..."

  while [ "${retries}" -ge 0 ]; do
    local recommender
    local admission
    local updater
    recommender=$(${KUBECTL} get deployment vpa-recommender-default -n "${namespace}" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)
    admission=$(${KUBECTL} get deployment vpa-admission-plugin-default -n "${namespace}" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)
    updater=$(${KUBECTL} get deployment vpa-updater-default -n "${namespace}" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)
    recommender=${recommender:-0}
    admission=${admission:-0}
    updater=${updater:-0}

    local ready=true

    # Check each controller against expected replica count
    [ "${recommender}" -ge "${recommender_expected}" ] || ready=false
    [ "${admission}" -ge "${admission_expected}" ] || ready=false
    [ "${updater}" -ge "${updater_expected}" ] || ready=false

    if [ "${ready}" == "true" ]; then
      echo "All required VPA controllers are ready"
      return 0
    fi

    retries=$((retries - 1))
    [ ${retries} -ge 0 ] && echo "${retries} retries left"
    sleep 5
  done

  echo "Current replicas: recommender=${recommender}, admission=${admission}, updater=${updater}"
  echo "Expected replicas: recommender=${recommender_expected}, admission=${admission_expected}, updater=${updater_expected}"
  return 1
}

function verify_operator_version() {
  echo "Verifying operator version is properly set..."

  local operator_logs
  operator_logs=$(${KUBECTL} logs -n "${namespace}" --selector k8s-app=vertical-pod-autoscaler-operator --tail=100)

  # Extract the version line from logs
  local version_line
  version_line=$(echo "${operator_logs}" | grep -E "Version.*version.*vertical-pod-autoscaler-operator" | head -1)

  if [ -z "${version_line}" ]; then
    echo "ERROR: Could not find version information in operator logs"
    echo ""
    echo "=== Operator Logs ==="
    echo "${operator_logs}"
    return 1
  fi

  echo "Found version line: ${version_line}"

  # Extract the actual version string (e.g., "v4.21.0-abc1234")
  local version_string
  version_string=$(echo "${version_line}" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+[-a-zA-Z0-9]*' | head -1)

  if [ -z "${version_string}" ]; then
    echo "ERROR: Could not extract version string from logs"
    echo "Version line was: ${version_line}"
    return 1
  fi

  echo "Extracted version: ${version_string}"

  # Verify it's not the default "was-not-built-properly" value
  if echo "${version_string}" | grep -q "was-not-built-properly"; then
    echo "ERROR: Operator version was not properly injected during build"
    echo "Got: ${version_string}"
    return 1
  fi

  # Verify it's a valid semver format (starts with v and has major.minor.patch)
  if ! echo "${version_string}" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+'; then
    echo "ERROR: Operator version is not in valid semver format"
    echo "Got: ${version_string}"
    return 1
  fi

  echo "Operator version is valid: ${version_string}"
  return 0
}

# Setup autoscaler repository
AUTOSCALER_PKG="github.com/openshift/kubernetes-autoscaler"
RELEASE_VERSION="release-4.22"

# Use cached repo if AUTOSCALER_TMP is set, otherwise clone fresh
# e.g. AUTOSCALER_TMP=/tmp/autoscaler
if [ -n "${AUTOSCALER_TMP:-}" ] && [ -d "${AUTOSCALER_TMP}" ]; then
  echo "Using cached autoscaler repo: ${AUTOSCALER_TMP}"
  
  if [ ! -d "${AUTOSCALER_TMP}/.git" ]; then
    echo "ERROR: AUTOSCALER_TMP exists but is not a git repository"
    exit 1
  fi
  GOPATH="$(dirname "${AUTOSCALER_TMP}")"
else
  echo "Cloning fresh autoscaler repo..."
  GOPATH="$(mktemp -d)"
  mkdir -p "${GOPATH}"
  
  git clone -b "${RELEASE_VERSION}" --single-branch \
    "https://${AUTOSCALER_PKG}.git" "${GOPATH}/autoscaler"
fi

export GOPATH
SCRIPT_ROOT="${GOPATH}/autoscaler/vertical-pod-autoscaler"

echo "Using GOPATH: ${GOPATH}"
echo "Using SCRIPT_ROOT: ${SCRIPT_ROOT}"

# Verify the operator version is properly set
echo ""
echo "================================"
echo "Verifying Operator Version"
echo "================================"

if ! verify_operator_version; then
  echo ""
  echo "ERROR: Operator version verification failed"
  exit 1
fi

# Verify VPA controllers are running
echo ""
echo "================================"
echo "Checking VPA Controller Status"
echo "================================"

disable_controllers

if ! wait_for_controllers 10; then
  echo ""
  echo "ERROR: VPA controllers failed to become ready"
  echo ""
  echo "=== Pods ==="
  ${KUBECTL} get pods -n "${namespace}"
  echo ""
  echo "=== Operator Logs ==="
  ${KUBECTL} logs -n "${namespace}" --selector k8s-app=vertical-pod-autoscaler-operator --tail=50
  exit 1
fi

# Run the upstream e2e tests
echo ""
echo "================================"
echo "Running VPA E2E Tests (${SUITE})"
echo "================================"

if ! run_upstream_vpa_tests; then
  echo "ERROR: VPA e2e tests failed"
  exit 1
fi

echo ""
echo "================================"
echo "All tests completed successfully!"
echo "================================"
