package operator

import (
	"fmt"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/vertical-pod-autoscaler-operator/pkg/apis"
	"github.com/openshift/vertical-pod-autoscaler-operator/pkg/controller/verticalpodautoscaler"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

// OperatorName is the name of this operator.
const OperatorName = "vertical-pod-autoscaler"

// Operator represents an instance of the vertical-pod-autoscaler-operator.
type Operator struct {
	config  *Config
	manager manager.Manager
}

// New returns a new Operator instance with the given config and a
// manager configured with the various controllers.
func New(cfg *Config) (*Operator, error) {
	operator := &Operator{config: cfg}

	// Get a config to talk to the apiserver.
	clientConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	// Create the controller-manager.
	managerOptions := manager.Options{
		Namespace: cfg.WatchNamespace,
	}

	operator.manager, err = manager.New(clientConfig, managerOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %v", err)
	}

	// Setup Scheme for all resources.
	if err := apis.AddToScheme(operator.manager.GetScheme()); err != nil {
		return nil, fmt.Errorf("failed to register types: %v", err)
	}

	// Setup our controllers and add them to the manager.
	if err := operator.AddControllers(); err != nil {
		return nil, fmt.Errorf("failed to add controllers: %v", err)
	}

	statusConfig := &StatusReporterConfig{
		VerticalPodAutoscalerName:      cfg.VerticalPodAutoscalerName,
		VerticalPodAutoscalerNamespace: cfg.VerticalPodAutoscalerNamespace,
		ReleaseVersion:                 cfg.ReleaseVersion,
		RelatedObjects:                 operator.RelatedObjects(),
	}

	statusReporter, err := NewStatusReporter(operator.manager, statusConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create status reporter: %v", err)
	}

	if err := operator.manager.Add(statusReporter); err != nil {
		return nil, fmt.Errorf("failed to add status reporter to manager: %v", err)
	}

	return operator, nil
}

// RelatedObjects returns a list of objects related to the operator and its
// operands.  These are used in the ClusterOperator status.
func (o *Operator) RelatedObjects() []configv1.ObjectReference {
	relatedNamespaces := map[string]string{}

	relatedNamespaces[o.config.WatchNamespace] = ""
	relatedNamespaces[o.config.VerticalPodAutoscalerNamespace] = ""

	relatedObjects := []configv1.ObjectReference{}

	for namespace := range relatedNamespaces {
		relatedObjects = append(relatedObjects, configv1.ObjectReference{
			Resource: "namespaces",
			Name:     namespace,
		})
	}

	return relatedObjects
}

// AddControllers configures the various controllers and adds them to
// the operator's manager instance.
func (o *Operator) AddControllers() error {
	// Setup VerticalPodAutoscalerController.
	vpa := verticalpodautoscaler.NewReconciler(o.manager, &verticalpodautoscaler.Config{
		ReleaseVersion: o.config.ReleaseVersion,
		Name:           o.config.VerticalPodAutoscalerName,
		Image:          o.config.VerticalPodAutoscalerImage,
		Namespace:      o.config.VerticalPodAutoscalerNamespace,
		Verbosity:      o.config.VerticalPodAutoscalerVerbosity,
		ExtraArgs:      o.config.VerticalPodAutoscalerExtraArgs,
	})

	if err := vpa.AddToManager(o.manager); err != nil {
		return err
	}

	return nil
}

// Start starts the operator's controller-manager.
func (o *Operator) Start() error {
	stopCh := signals.SetupSignalHandler()

	return o.manager.Start(stopCh)
}
