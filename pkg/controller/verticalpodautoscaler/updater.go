package verticalpodautoscaler

import (
	"fmt"
	"github.com/openshift/vertical-pod-autoscaler-operator/pkg/apis/autoscaling/v1"
)

// UpdaterArg represents a command line argument to the VPA's recommender
// that may be combined with a value or numerical range.
type UpdaterArg string

// String returns the argument as a plain string.
func (a UpdaterArg) String() string {
	return string(a)
}

// Value returns the argument with the given value set.
func (a UpdaterArg) Value(v interface{}) string {
	return fmt.Sprintf("%s=%v", a.String(), v)
}

// UpdaterArgs returns a slice of strings representing command line arguments
// to the recommnder corresponding to the values in the given
// VerticalPodAutoscalerController resource.
func UpdaterArgs(vpa *v1.VerticalPodAutoscalerController, cfg *Config) []string {
	args := []string{
		LogToStderrArg.String(),
		VerbosityArg.Value(cfg.Verbosity),
	}
	return args
}
