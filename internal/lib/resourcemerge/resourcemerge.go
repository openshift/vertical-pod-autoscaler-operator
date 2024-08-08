package resourcemerge

// TODO(jkyros): This was originally in the CVO, and we were using it from there, but they did some cleanup in https://github.com/openshift/cluster-version-operator/pull/1012
// and now it's not there anymore. But we're still using it, so I moved it here. There wasn't a generic function in library-go we should be using instead, so it's ours now.

import (
	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FindOperatorStatusCondition searches through the list of conditions to find a condition of the requested type
func FindOperatorStatusCondition(conditions []configv1.ClusterOperatorStatusCondition, conditionType configv1.ClusterStatusConditionType) *configv1.ClusterOperatorStatusCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}

// EnsureClusterOperatorStatus ensures that the existing cluster operator status matches the required cluster operator status, setting
// the "modified" boolean argument to true if existing was not equal to required had to be updated
func EnsureClusterOperatorStatus(modified *bool, existing *configv1.ClusterOperator, required configv1.ClusterOperator) {
	EnsureObjectMeta(modified, &existing.ObjectMeta, required.ObjectMeta)
	ensureClusterOperatorStatus(modified, &existing.Status, required.Status)
}

func ensureClusterOperatorStatus(modified *bool, existing *configv1.ClusterOperatorStatus, required configv1.ClusterOperatorStatus) {
	if !equality.Semantic.DeepEqual(existing.Conditions, required.Conditions) {
		*modified = true
		existing.Conditions = required.Conditions
	}

	if !equality.Semantic.DeepEqual(existing.Versions, required.Versions) {
		*modified = true
		existing.Versions = required.Versions
	}
	if !equality.Semantic.DeepEqual(existing.Extension.Raw, required.Extension.Raw) {
		*modified = true
		existing.Extension.Raw = required.Extension.Raw
	}
	if !equality.Semantic.DeepEqual(existing.Extension.Object, required.Extension.Object) {
		*modified = true
		existing.Extension.Object = required.Extension.Object
	}
	if !equality.Semantic.DeepEqual(existing.RelatedObjects, required.RelatedObjects) {
		*modified = true
		existing.RelatedObjects = required.RelatedObjects
	}
}

// EnsureObjectMeta ensures that the existing matches the required.
// modified is set to true when existing had to be updated with required.
func EnsureObjectMeta(modified *bool, existing *metav1.ObjectMeta, required metav1.ObjectMeta) {
	setStringIfSet(modified, &existing.Namespace, required.Namespace)
	setStringIfSet(modified, &existing.Name, required.Name)
	mergeMap(modified, &existing.Labels, required.Labels)
	mergeMap(modified, &existing.Annotations, required.Annotations)
	mergeOwnerRefs(modified, &existing.OwnerReferences, required.OwnerReferences)
}

func setStringIfSet(modified *bool, existing *string, required string) {
	if len(required) == 0 {
		return
	}
	if required != *existing {
		*existing = required
		*modified = true
	}
}

func mergeMap(modified *bool, existing *map[string]string, required map[string]string) {
	if *existing == nil {
		if required == nil {
			return
		}
		*existing = map[string]string{}
	}
	for k, v := range required {
		if existingV, ok := (*existing)[k]; !ok || v != existingV {
			*modified = true
			(*existing)[k] = v
		}
	}
}

func mergeOwnerRefs(modified *bool, existing *[]metav1.OwnerReference, required []metav1.OwnerReference) {
	for ridx := range required {
		found := false
		for eidx := range *existing {
			if required[ridx].UID == (*existing)[eidx].UID {
				found = true
				if !equality.Semantic.DeepEqual((*existing)[eidx], required[ridx]) {
					*modified = true
					(*existing)[eidx] = required[ridx]
				}
				break
			}
		}
		if !found {
			*modified = true
			*existing = append(*existing, required[ridx])
		}
	}
}
