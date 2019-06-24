package verticalpodautoscaler

import (
	"fmt"
	"github.com/openshift/vertical-pod-autoscaler-operator/pkg/apis/autoscaling/v1"
)

// RecommenderArg represents a command line argument to the VPA's recommender
// that may be combined with a value or numerical range.
type RecommenderArg string

// String returns the argument as a plain string.
func (a RecommenderArg) String() string {
	return string(a)
}

// Value returns the argument with the given value set.
func (a RecommenderArg) Value(v interface{}) string {
	return fmt.Sprintf("%s=%v", a.String(), v)
}

// These constants represent the vertical-pod-autoscaler arguments used by the
// operator when processing VerticalPodAutoscalerController resources.
const (
	LogToStderrArg          RecommenderArg = "--logtostderr"
	VerbosityArg            RecommenderArg = "--v"
	SafetyMarginFractionArg RecommenderArg = "--recommendation-margin-fraction"
	PodMinCPUMillicoresArg  RecommenderArg = "--pod-recommendation-min-cpu-millicores"
	PodMinMemoryMbArg       RecommenderArg = "--pod-recommendation-min-memory-mb"
)

// RecommenderArgs returns a slice of strings representing command line arguments
// to the recommnder corresponding to the values in the given
// VerticalPodAutoscalerController resource.
func RecommenderArgs(vpa *v1.VerticalPodAutoscalerController, cfg *Config) []string {
	s := &vpa.Spec

	args := []string{
		LogToStderrArg.String(),
		VerbosityArg.Value(cfg.Verbosity),
	}
	if s.SafetyMarginFraction != nil {
		v := SafetyMarginFractionArg.Value(*s.SafetyMarginFraction)
		args = append(args, v)
	}
	if s.PodMinCPUMillicores != nil {
		v := PodMinCPUMillicoresArg.Value(*s.PodMinCPUMillicores)
		args = append(args, v)
	}
	if s.PodMinMemoryMb != nil {
		v := PodMinMemoryMbArg.Value(*s.PodMinMemoryMb)
		args = append(args, v)
	}

	return args
}
