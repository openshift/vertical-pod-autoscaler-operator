package operator

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	osconfig "github.com/openshift/client-go/config/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

const (
	// InfrastructureName is the name of the singleton Infrastructure resource
	InfrastructureName = "cluster"
)

// GetControlPlaneTopology fetches the Infrastructure resource and returns
// the control plane topology. Returns ExternalTopologyMode for HCP clusters.
func GetControlPlaneTopology(config *rest.Config) (configv1.TopologyMode, error) {
	configClient, err := osconfig.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create config client: %w", err)
	}

	infra, err := configClient.ConfigV1().Infrastructures().Get(
		context.TODO(),
		InfrastructureName,
		metav1.GetOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to get Infrastructure resource: %w", err)
	}

	topology := infra.Status.ControlPlaneTopology
	klog.Infof("Detected control plane topology: %s", topology)
	return topology, nil
}

// IsExternalControlPlane returns true if the cluster has an external
// control plane (HCP/Hosted Control Plane topology).
func IsExternalControlPlane(config *rest.Config) bool {
	topology, err := GetControlPlaneTopology(config)
	if err != nil {
		// Log warning but default to standard topology for safety
		klog.Warningf("Failed to detect control plane topology, assuming standard: %v", err)
		return false
	}
	return topology == configv1.ExternalTopologyMode
}
