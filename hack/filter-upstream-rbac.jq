# Add OpenShift-specific deploymentconfigs perms to vpa-target-reader
(.items[] | select(.kind=="ClusterRole" and .metadata.name=="system:vpa-target-reader")).rules += [ {
        apiGroups: [ "apps.openshift.io" ],
        resources: [ "deploymentconfigs", "deploymentconfigs/scale" ],
        verbs: [ "get", "list", "watch" ] } ] |
# Security fix: Remove wildcard apiGroups from vpa-target-reader
(.items[] | select(.kind=="ClusterRole" and .metadata.name=="system:vpa-target-reader")).rules |=
  map(select(.apiGroups != ["*"])) |
# Security fix: Remove patch/update from events in vpa-actor
(.items[] | select(.kind=="ClusterRole" and .metadata.name=="system:vpa-actor")).rules |=
  map(if .resources == ["events"] then .verbs |= (. - ["patch", "update"]) else . end) |
# Security fix: Remove general pods patch from vpa-updater-in-place
(.items[] | select(.kind=="ClusterRole" and .metadata.name=="system:vpa-updater-in-place")).rules |=
  map(if .resources then .resources |= (. - ["pods"]) else . end) |
# We use namespace openshift-vertical-pod-autoscaler instead of kube-system. Replace all namespaces
walk(if type == "object" and has("namespace") then .namespace="openshift-vertical-pod-autoscaler" else . end)
