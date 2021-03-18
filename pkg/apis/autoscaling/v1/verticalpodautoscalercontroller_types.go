package v1

import (
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
	MinReplicas        *int64   `json:"minReplicas,omitempty"`
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
