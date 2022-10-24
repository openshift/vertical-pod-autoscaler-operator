def indexof(f):
  first(range(0;length) as $i
        | select(.[$i]|f) | $i) // null;

# Find the ClusterRoleBinding with the typo name
(.items | indexof(.kind=="ClusterRoleBinding" and .metadata.name=="system:vpa-evictionter-binding")) as $i |
# Fix the typo
if $i then .items[$i].metadata.name |= "system:vpa-evictioner-binding" else . end |
# insert two missing ServiceAccounts after the ClusterRoleBinding with the typo name 
if $i then (.items |= .[0:$i+1] + [
        {apiVersion: "v1", kind: "ServiceAccount", metadata: {name: "vpa-updater", namespace: "kube-system"}},
        {apiVersion: "v1", kind: "ServiceAccount", metadata: {name: "vpa-recommender", namespace: "kube-system"}}
      ] + .[$i+1:]) else . end |
# Add OpenShift-specific deploymentconfigs perms to vpa-target-reader
(.items[] | select(.kind=="ClusterRole" and .metadata.name=="system:vpa-target-reader")).rules += [ {
        apiGroups: [ "apps.openshift.io" ],
        resources: [ "deploymentconfigs", "deploymentconfigs/scale" ],
        verbs: [ "get", "list", "watch" ] } ] |
# We use namespace openshift-vertical-pod-autoscaler instead of kube-system. Replace all namespaces
walk(if type == "object" and has("namespace") then .namespace="openshift-vertical-pod-autoscaler" else . end)
