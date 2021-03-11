# sort a CSV-style permissions list (i.e. permissions or clusterPermissions) so it can be compared to another sorted list
def sortpermission:
  # add a temporary sortKey field to each rule. The sortKey is a combination of all apiGroups, resources and verbs
  [ .[] | .rules[] |= (. + {sortKey: ((.apiGroups|join(","))+":"+(.resources|join(","))+":"+(.verbs|join(","))) }) ] |
  # filter out permissions with no rules, then sort first by service account name
  [ .[] | select(.rules|length > 0) ] | sort_by(.serviceAccountName) |
  # do secondary sorting by the temporary sortKey
  [ .[] | .rules|=sort_by(.sortKey) ] |
  # sort the items in the 3 lists (apiGroups, resources, verbers) and drop the temporary sortKey
  [ .[] |
    {
      serviceAccountName: .serviceAccountName,
      rules: [ .rules[] | { apiGroups: .apiGroups | sort, resources: .resources | sort, verbs: .verbs | sort } ]
    }
  ];

# convert a list of RBAC objects (such as ServiceAccount, (Cluster)Role, (Cluster)RoleBinding) to CSV-style permissions list
# pass in "ClusterRole" or "Role" for $roletype
def objlist2permission($roletype):
  . as $objlist |
  [
    # for each service account
    $objlist[] | select(.kind=="ServiceAccount") | .metadata.name as $sa |
    # create a permission object
    {
      serviceAccountName: $sa,
      rules: [
        # find all the rolebindings (or clusterrolebindings) that match the service account
        $objlist[] | select(.kind == ($roletype + "Binding")) | select(any(.subjects[]; .kind=="ServiceAccount" and .name==$sa)) | .roleRef.name as $r |
        # then find the role (or clusterrole) that the rolebinding referred to, and add its rules to the permission
        $objlist[] | select(.kind == $roletype) | select(.metadata.name==$r) | .rules[]
      ]
    }
  ];

# detect if we have a list of objects or a csv file
( if .kind == "List" then
    (.items | objlist2permission("ClusterRole"))
  else
    if .kind == "ClusterServiceVersion" then
      .spec.install.spec.clusterPermissions
    else
      error("Expected ClusterServiceVersion or List of RBAC objects")
    end
  end ) as $cperm |
( if .kind == "List" then
    (.items | objlist2permission("Role"))
  else
    if .kind == "ClusterServiceVersion" then
      .spec.install.spec.permissions
    else
      error("Expected ClusterServiceVersion or List of RBAC objects")
    end
  end ) as $perm |
{
  clusterPermissions: $cperm | sortpermission,
  permissions: $perm | sortpermission
}
