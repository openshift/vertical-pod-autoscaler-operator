package verticalpodautoscaler

import (
	"fmt"

	v1 "github.com/openshift/vertical-pod-autoscaler-operator/api/v1"
)

// AdmissionPluginArg represents a command line argument to the VPA's recommender
// that may be combined with a value or numerical range.
type AdmissionPluginArg string

// These constants represent the vertical-pod-autoscaler arguments used by the
// operator when processing VerticalPodAutoscalerController resources.
const (
	TLSCertFileArg   AdmissionPluginArg = "--tls-cert-file"
	TLSKeyFileArg    AdmissionPluginArg = "--tls-private-key"
	TLSCACertFileArg AdmissionPluginArg = "--client-ca-file"
	WebhookTimeout   AdmissionPluginArg = "--webhook-timeout-seconds"
)

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
		TLSCertFileArg.Value("/data/tls-certs/tls.crt"),
		TLSKeyFileArg.Value("/data/tls-certs/tls.key"),
		TLSCACertFileArg.Value("/data/tls-ca-certs/service-ca.crt"),
		WebhookTimeout.Value("10"),
	}
	return args
}
