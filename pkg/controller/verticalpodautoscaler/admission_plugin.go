package verticalpodautoscaler

import (
	"fmt"
	"github.com/openshift/vertical-pod-autoscaler-operator/pkg/apis/autoscaling/v1"
)

// AdmissionPluginArg represents a command line argument to the VPA's recommender
// that may be combined with a value or numerical range.
type AdmissionPluginArg string

// String returns the argument as a plain string.
func (a AdmissionPluginArg) String() string {
	return string(a)
}

// Value returns the argument with the given value set.
func (a AdmissionPluginArg) Value(v interface{}) string {
	return fmt.Sprintf("%s=%v", a.String(), v)
}

// AdmissionPluginArgs returns a slice of strings representing command line arguments
// to the recommnder corresponding to the values in the given
// VerticalPodAutoscalerController resource.
func AdmissionPluginArgs(vpa *v1.VerticalPodAutoscalerController, cfg *Config) []string {
	args := []string{
		LogToStderrArg.String(),
		VerbosityArg.Value(cfg.Verbosity),
	}
	return args
}
