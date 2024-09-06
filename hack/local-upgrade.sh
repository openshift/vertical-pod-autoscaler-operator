#!/bin/bash

# Required environment variables:
# - PREVIOUS_BUNDLE: The bundle image to install
# - OO_BUNDLE: The bundle image to upgrade to
# - KUBECONFIG: The kubeconfig file to use for the test

echo PREVIOUS_BUNDLE: "$PREVIOUS_BUNDLE"
echo OO_BUNDLE: "$OO_BUNDLE"
echo KUBECONFIG: "$KUBECONFIG"
sleep 3

### Install step

export VPA_NS=openshift-vertical-pod-autoscaler
oc create ns $VPA_NS

operator-sdk run bundle --timeout=10m -n $VPA_NS --security-context-config restricted "$PREVIOUS_BUNDLE" || true

oc wait --timeout=10m --for condition=Available -n $VPA_NS deployment vertical-pod-autoscaler-operator

### Upgrade step

echo "Upgrading the operator..."
sleep 3

operator-sdk run bundle-upgrade --timeout 10m -n $VPA_NS --security-context-config restricted "$OO_BUNDLE" || true

oc wait --timeout=10m --for condition=Available -n $VPA_NS deployment vertical-pod-autoscaler-operator

### Test step

make e2e-olm-ci
