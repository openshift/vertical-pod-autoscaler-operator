package operator

import (
	"os"
	"strconv"

	"k8s.io/klog"
)

const (
	// DefaultWatchNamespace is the default namespace the operator
	// will watch for instances of its custom resources.
	DefaultWatchNamespace = "openshift-vertical-pod-autoscaler"

	// DefaultVerticalPodAutoscalerNamespace is the default namespace for
	// vertical-pod-autoscaler deployments.
	DefaultVerticalPodAutoscalerNamespace = "openshift-vertical-pod-autoscaler"

	// DefaultVerticalPodAutoscalerName is the default VerticalPodAutoscalerController
	// object watched by the operator.
	DefaultVerticalPodAutoscalerName = "default"

	// DefaultVerticalPodAutoscalerImage is the default image used in
	// VerticalPodAutoscalerController deployments.
	DefaultVerticalPodAutoscalerImage = "quay.io/openshift/origin-vertical-pod-autoscaler:4.16.0"

	// DefaultVerticalPodAutoscalerVerbosity is the default logging
	// verbosity level for VerticalPodAutoscalerController deployments.
	DefaultVerticalPodAutoscalerVerbosity = 1
)

// Config represents the runtime configuration for the operator.
type Config struct {
	// ReleaseVersion is the version the operator is expected
	// to report once it has reached level.
	ReleaseVersion string

	// WatchNamespace is the namespace the operator will watch for
	// VerticalPodAutoscalerController instances.
	WatchNamespace string

	// VerticalPodAutoscalerNamespace is the namespace in which
	// vertical-pod-autoscaler deployments will be created.
	VerticalPodAutoscalerNamespace string

	// VerticalPodAutoscalerName is the name of the VerticalPodAutoscalerController
	// resource that will be watched by the operator.
	VerticalPodAutoscalerName string

	// VerticalPodAutoscalerImage is the image to be used in
	// VerticalPodAutoscalerController deployments.
	VerticalPodAutoscalerImage string

	// VerticalPodAutoscalerVerbosity is the logging verbosity level for
	// VerticalPodAutoscalerController deployments.
	VerticalPodAutoscalerVerbosity int

	// VerticalPodAutoscalerExtraArgs is a string of additional arguments
	// passed to all VerticalPodAutoscalerController deployments.
	//
	// This is not exposed in the CRD.  It is only configurable via
	// environment variable, and in a normal OpenShift install the CVO
	// will remove it if set manually.  It is only for development and
	// debugging purposes.
	VerticalPodAutoscalerExtraArgs string
}

// NewConfig returns a new Config object with defaults set.
func NewConfig() *Config {
	return &Config{
		WatchNamespace:                 DefaultWatchNamespace,
		VerticalPodAutoscalerNamespace: DefaultVerticalPodAutoscalerNamespace,
		VerticalPodAutoscalerName:      DefaultVerticalPodAutoscalerName,
		VerticalPodAutoscalerImage:     DefaultVerticalPodAutoscalerImage,
		VerticalPodAutoscalerVerbosity: DefaultVerticalPodAutoscalerVerbosity,
	}
}

// ConfigFromEnvironment returns a new Config object with defaults
// overridden by environment variables when set.
func ConfigFromEnvironment() *Config {
	config := NewConfig()

	if releaseVersion, ok := os.LookupEnv("RELEASE_VERSION"); ok {
		config.ReleaseVersion = releaseVersion
	}

	if watchNamespace, ok := os.LookupEnv("WATCH_NAMESPACE"); ok {
		config.WatchNamespace = watchNamespace
	}

	if caName, ok := os.LookupEnv("VERTICAL_POD_AUTOSCALER_NAME"); ok {
		config.VerticalPodAutoscalerName = caName
	}

	if caImage, ok := os.LookupEnv("RELATED_IMAGE_VPA"); ok {
		config.VerticalPodAutoscalerImage = caImage
	}

	if caNamespace, ok := os.LookupEnv("VERTICAL_POD_AUTOSCALER_NAMESPACE"); ok {
		config.VerticalPodAutoscalerNamespace = caNamespace
	}

	if caVerbosity, ok := os.LookupEnv("VERTICAL_POD_AUTOSCALER_VERBOSITY"); ok {
		v, err := strconv.Atoi(caVerbosity)
		if err != nil {
			v = DefaultVerticalPodAutoscalerVerbosity
			klog.Errorf("Error parsing VERTICAL_POD_AUTOSCALER_VERBOSITY environment variable: %v", err)
		}

		config.VerticalPodAutoscalerVerbosity = v
	}

	if caExtraArgs, ok := os.LookupEnv("VERTICAL_POD_AUTOSCALER_EXTRA_ARGS"); ok {
		config.VerticalPodAutoscalerExtraArgs = caExtraArgs
	}

	return config
}
