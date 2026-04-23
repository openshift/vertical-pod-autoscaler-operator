/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

// VerticalPodAutoscalerControllerSpec defines the desired state of VerticalPodAutoscalerController
type VerticalPodAutoscalerControllerSpec struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Safety Margin Fraction",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	// +kubebuilder:validation:Minimum=0
	SafetyMarginFraction *float64 `json:"safetyMarginFraction,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Minimum CPU (millicores)",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	// +kubebuilder:validation:Minimum=0
	PodMinCPUMillicores *float64 `json:"podMinCPUMillicores,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Minimum Memory (MB)",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	// +kubebuilder:validation:Minimum=0
	PodMinMemoryMb     *float64 `json:"podMinMemoryMb,omitempty"`
	RecommendationOnly *bool    `json:"recommendationOnly,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Minimum Replicas",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	// +kubebuilder:validation:Minimum=1
	MinReplicas *int64 `json:"minReplicas,omitempty"`
	//
	// +optional
	DeploymentOverrides DeploymentOverrides `json:"deploymentOverrides"`
}

// VerticalPodAutoscalerControllerStatus defines the observed state of VerticalPodAutoscalerController
type VerticalPodAutoscalerControllerStatus struct {
	// TODO: Add status fields.
}

// +kubebuilder:object:root=true
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// Represents an instance of the set of VPA controllers
// +operator-sdk:csv:customresourcedefinitions:displayName="VPA Controller"
// +operator-sdk:csv:customresourcedefinitions:resources={{Deployment,v1,""},{Service,v1,""}}
type VerticalPodAutoscalerController struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticalPodAutoscalerControllerSpec   `json:"spec,omitempty"`
	Status VerticalPodAutoscalerControllerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VerticalPodAutoscalerControllerList contains a list of VerticalPodAutoscalerController
type VerticalPodAutoscalerControllerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticalPodAutoscalerController `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticalPodAutoscalerController{}, &VerticalPodAutoscalerControllerList{})
}
