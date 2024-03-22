package v1

import (
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&VerticalPodAutoscalerController{}, &VerticalPodAutoscalerControllerList{})
}

// VerticalPodAutoscalerSpec defines the desired state of VerticalPodAutoscalerController
type VerticalPodAutoscalerSpec struct {
	// +kubebuilder:validation:Minimum=0
	SafetyMarginFraction *float64 `json:"safetyMarginFraction,omitempty"`
	// +kubebuilder:validation:Minimum=0
	PodMinCPUMillicores *float64 `json:"podMinCPUMillicores,omitempty"`
	// +kubebuilder:validation:Minimum=0
	PodMinMemoryMb     *float64 `json:"podMinMemoryMb,omitempty"`
	RecommendationOnly *bool    `json:"recommendationOnly,omitempty"`
	// +kubebuilder:validation:Minimum=1
	MinReplicas *int64 `json:"minReplicas,omitempty"`
	//
	// +optional
	DeploymentOverrides DeploymentOverrides `json:"deploymentOverrides"`
}

// DeploymentOverrides defines overrides for deployments managed by the VerticalPodAutoscalerController
type DeploymentOverrides struct {

	// admission is the deployment overrides for the VPA's admission container
	// +optional
	Admission DeploymentOverride `json:"admission"`
	// recommender is the deployment overrides for the VPA's recommender container
	// +optional
	Recommender DeploymentOverride `json:"recommender"`
	// updater is the deployment overrides for the VPA's updater container
	// +optional
	Updater DeploymentOverride `json:"updater"`
}

// DeploymentOverride defines fields that can be overridden for a given deployment
type DeploymentOverride struct {
	// TODO(jkyros): appsv1.DeploymentSpec fields can go here someday

	// We'd love to just use the deployment spec to override container fields, but it's really
	// unwieldy having a user fill out the container array and have to merge them by name, so we include
	// a specific container override field.

	// Container allows for direct overrides on the "important" container of the deployment, i.e. the one
	// that is actually running the operand
	// +optional
	Container ContainerOverride `json:"container"`

	// Override the NodeSelector of the deployment's pod. This allows, for example, for the VPA controllers
	// to be run on non-master nodes
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Override the Tolerations of the deployment's pod. This allows, for example, for the VPA controllers
	// to be run on non-master nodes with a specific taint
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// ContainerOverride defines fields that can be overridden for a given container
type ContainerOverride struct {
	// TODO(jkyros): maybe this eventually ends up being the whole corev1.Container, so try
	// to keep the fields equivalent. I'd just make this a corev1.Container, but we'd have to
	// silently drop fields we don't support and I'd rather not be confusing

	// args is a list of args that will be appended to the container spec
	// +optional
	Args []string `json:"args,omitempty"`
	// resources is a set of resource requirements that will replace existing container resource requirements
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// VerticalPodAutoscalerStatus defines the observed state of VerticalPodAutoscalerController
type VerticalPodAutoscalerStatus struct {
	// TODO: Add status fields.
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VerticalPodAutoscalerController is the Schema for the verticalpodautoscalerControllers API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type VerticalPodAutoscalerController struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticalPodAutoscalerSpec   `json:"spec,omitempty"`
	Status VerticalPodAutoscalerStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VerticalPodAutoscalerControllerList contains a list of VerticalPodAutoscalerController
type VerticalPodAutoscalerControllerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticalPodAutoscalerController `json:"items"`
}
