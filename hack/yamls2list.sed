# Transform concatenated yaml files of k8s objects into a k8s List
1 i \
---\nkind: List\nmetadata: {}\napiVersion: v1\nitems:
s/^/  /
/^  ---/,+1 {
  /^  ---/ d
  s/^ /-/
}
