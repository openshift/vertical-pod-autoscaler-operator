# Add OpenShift-specific deploymentconfigs perms to vpa-target-reader
(.items[] | select(.kind=="ClusterRole" and .metadata.name=="system:vpa-target-reader")).rules += [ {
        apiGroups: [ "apps.openshift.io" ],
        resources: [ "deploymentconfigs", "deploymentconfigs/scale" ],
        verbs: [ "get", "list", "watch" ] } ] |
# Remove webhook permissions from admission controller (operator manages webhook now)
(.items[] | select(.kind=="ClusterRole" and .metadata.name=="system:vpa-admission-controller")).rules |= (
  map(select(.apiGroups != ["admissionregistration.k8s.io"] or .resources != ["mutatingwebhookconfigurations"]))
) |
# We use namespace openshift-vertical-pod-autoscaler instead of kube-system. Replace all namespaces
walk(if type == "object" and has("namespace") then .namespace="openshift-vertical-pod-autoscaler" else . end)
