# Transform concatenated yaml files of k8s objects into a k8s List
# Add kind, metadata and items at the top
1 i\
---\
kind: List\
metadata: {}\
apiVersion: v1\
items:
# indent every existing line by 2 spaces
s/^/  /
# Any line which was originally "---" should be deleted and the following line should begin with "- " since it's the first line of an item in the list
/^  ---/,+1 {
  /^  ---/ d
  s/^ /-/
}
