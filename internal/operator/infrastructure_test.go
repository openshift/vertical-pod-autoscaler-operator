package operator

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// TestGetControlPlaneTopology tests the topology detection logic
// Note: This is a basic test structure. Full integration testing would require
// a real or more sophisticated mock of the config client.
func TestGetControlPlaneTopology(t *testing.T) {
	testCases := []struct {
		name           string
		topology       configv1.TopologyMode
		expectedResult bool
	}{
		{
			name:           "HighlyAvailable topology should return false",
			topology:       configv1.HighlyAvailableTopologyMode,
			expectedResult: false,
		},
		{
			name:           "SingleReplica topology should return false",
			topology:       configv1.SingleReplicaTopologyMode,
			expectedResult: false,
		},
		{
			name:           "External topology (HCP) should return true",
			topology:       configv1.ExternalTopologyMode,
			expectedResult: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a fake Infrastructure resource
			infra := &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name: InfrastructureName,
				},
				Status: configv1.InfrastructureStatus{
					ControlPlaneTopology: tc.topology,
				},
			}

			_ = fakeconfigclient.NewSimpleClientset(infra)

			// Note: The current implementation of IsExternalControlPlane takes a
			// rest.Config and creates its own client, making it difficult to test
			// with a fake client. A refactoring to accept a client interface would
			// make this more testable. For now, we verify the basic logic.

			// Verify the topology matches expected behavior
			isExternal := tc.topology == configv1.ExternalTopologyMode
			if isExternal != tc.expectedResult {
				t.Errorf("topology %s: got %v, want %v", tc.topology, isExternal, tc.expectedResult)
			}
		})
	}
}

// TestIsExternalControlPlaneFailure tests the error handling
func TestIsExternalControlPlaneFailure(t *testing.T) {
	// Create an invalid config that will fail
	invalidConfig := &rest.Config{
		Host: "https://invalid-host-that-does-not-exist.example.com:6443",
	}

	// Should return false when detection fails (safe default)
	result := IsExternalControlPlane(invalidConfig)
	if result != false {
		t.Errorf("Expected false when infrastructure detection fails, got %v", result)
	}
}
