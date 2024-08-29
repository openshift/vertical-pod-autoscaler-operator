package operator

import (
	"context"
	"fmt"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	osconfig "github.com/openshift/client-go/config/clientset/versioned"
	autoscalingv1 "github.com/openshift/vertical-pod-autoscaler-operator/api/v1"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/lib/resourcemerge"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/util"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorName = "vertical-pod-autoscaler"

// Reason messages used in status conditions.
const (
	ReasonEmpty             = ""
	ReasonMissingDependency = "MissingDependency"
	ReasonSyncing           = "SyncingResources"
	ReasonCheckAutoscaler   = "UnableToCheckAutoscalers"
)

// StatusReporter reports the status of the operator to the OpenShift
// cluster-version-operator via ClusterOperator resource status.
type StatusReporter struct {
	client       client.Client
	configClient osconfig.Interface
	config       *StatusReporterConfig
}

// StatusReporterConfig represents the configuration of a given StatusReporter.
type StatusReporterConfig struct {
	VerticalPodAutoscalerName      string
	VerticalPodAutoscalerNamespace string
	ReleaseVersion                 string
	RelatedObjects                 []configv1.ObjectReference
}

// NewStatusReporter returns a new StatusReporter instance.
func NewStatusReporter(mgr manager.Manager, cfg *StatusReporterConfig) (*StatusReporter, error) {
	var err error

	reporter := &StatusReporter{
		client: mgr.GetClient(),
		config: cfg,
	}

	// Create a client for OpenShift config objects.
	reporter.configClient, err = osconfig.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	return reporter, nil
}

// SetReleaseVersion sets the configured release version.
func (r *StatusReporter) SetReleaseVersion(version string) {
	r.config.ReleaseVersion = version
}

// SetRelatedObjects sets the configured related objects.
func (r *StatusReporter) SetRelatedObjects(objs []configv1.ObjectReference) {
	r.config.RelatedObjects = objs
}

// AddRelatedObjects adds to the list of related objects.
func (r *StatusReporter) AddRelatedObjects(objs []configv1.ObjectReference) {
	r.config.RelatedObjects = append(r.config.RelatedObjects, objs...)
}

// GetClusterOperator fetches the the operator's ClusterOperator object.
func (r *StatusReporter) GetClusterOperator() (*configv1.ClusterOperator, error) {
	return r.configClient.ConfigV1().ClusterOperators().Get(context.TODO(), OperatorName, metav1.GetOptions{})
}

// GetOrCreateClusterOperator gets, or if necessary, creates the
// operator's ClusterOperator object and returns it.
func (r *StatusReporter) GetOrCreateClusterOperator() (*configv1.ClusterOperator, error) {
	clusterOperator := &configv1.ClusterOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name: OperatorName,
		},
	}

	existing, err := r.GetClusterOperator()

	if errors.IsNotFound(err) {
		return r.configClient.ConfigV1().ClusterOperators().Create(context.TODO(), clusterOperator, metav1.CreateOptions{})
	}

	return existing, err
}

// ApplyStatus applies the given ClusterOperator status to the operator's
// ClusterOperator object if necessary.  The currently configured RelatedObjects
// are automatically set on the status.  If no ClusterOperator objects exists,
// one is created.
func (r *StatusReporter) ApplyStatus(status configv1.ClusterOperatorStatus) error {
	var modified bool

	co, err := r.GetOrCreateClusterOperator()
	if err != nil {
		return err
	}

	// Set the currently configured related objects.
	status.RelatedObjects = r.config.RelatedObjects

	// If no versions were set explicitly, continue reporting previous versions.
	if status.Versions == nil {
		status.Versions = co.Status.Versions
	}

	// Update LastTransitionTime for all conditions if necessary.
	for i := range status.Conditions {
		condType := status.Conditions[i].Type
		timestamp := metav1.NewTime(time.Now())

		c := resourcemerge.FindOperatorStatusCondition(co.Status.Conditions, condType)

		// If found, and status doesn't match, update.
		if c != nil && c.Status != status.Conditions[i].Status {
			status.Conditions[i].LastTransitionTime = timestamp
		}

		// If found, and status matches, copy previous.
		if c != nil && c.Status == status.Conditions[i].Status {
			status.Conditions[i].LastTransitionTime = c.LastTransitionTime
		}

		// If it's still nil, update it.
		if status.Conditions[i].LastTransitionTime.IsZero() {
			status.Conditions[i].LastTransitionTime = timestamp
		}
	}

	// If any versions have changed, we need to reset the transition time on the
	// Progressing condition whether or not we actually had any work to do.
	if !equality.Semantic.DeepEqual(status.Versions, co.Status.Versions) {
		util.ResetProgressingTime(&status.Conditions)
	}

	// Copy the current ClusterOperator and overwrite the status.
	requiredCO := &configv1.ClusterOperator{}
	co.DeepCopyInto(requiredCO)
	requiredCO.Status = status

	resourcemerge.EnsureClusterOperatorStatus(&modified, co, *requiredCO)

	if modified {
		_, err := r.configClient.ConfigV1().ClusterOperators().UpdateStatus(context.TODO(), co, metav1.UpdateOptions{})
		return err
	}

	return nil
}

// available reports the operator as available, not progressing, and
// not degraded -- optionally setting a reason and message.  This will
// update the reported operator version.  It should only be called if
// the operands are fully updated and available.
func (r *StatusReporter) available(reason, message string) error {
	status := configv1.ClusterOperatorStatus{
		Conditions: []configv1.ClusterOperatorStatusCondition{
			{
				Type:    configv1.OperatorAvailable,
				Status:  configv1.ConditionTrue,
				Reason:  reason,
				Message: message,
			},
			{
				Type:   configv1.OperatorProgressing,
				Status: configv1.ConditionFalse,
			},
			{
				Type:   configv1.OperatorDegraded,
				Status: configv1.ConditionFalse,
			},
		},
		Versions: []configv1.OperandVersion{
			{
				Name:    "operator",
				Version: r.config.ReleaseVersion,
			},
		},
	}

	klog.Infof("Operator status available: %s", message)

	return r.ApplyStatus(status)
}

// degraded reports the operator as degraded but available, and not
// progressing -- optionally setting a reason and message.
func (r *StatusReporter) degraded(reason, message string) error {
	status := configv1.ClusterOperatorStatus{
		Conditions: []configv1.ClusterOperatorStatusCondition{
			{
				Type:   configv1.OperatorAvailable,
				Status: configv1.ConditionTrue,
			},
			{
				Type:   configv1.OperatorProgressing,
				Status: configv1.ConditionFalse,
			},
			{
				Type:    configv1.OperatorDegraded,
				Status:  configv1.ConditionTrue,
				Reason:  reason,
				Message: message,
			},
		},
	}

	klog.Warningf("Operator status degraded: %s", message)

	return r.ApplyStatus(status)
}

// progressing reports the operator as progressing but available, and not
// degraded -- optionally setting a reason and message.
func (r *StatusReporter) progressing(reason, message string) error {
	status := configv1.ClusterOperatorStatus{
		Conditions: []configv1.ClusterOperatorStatusCondition{
			{
				Type:   configv1.OperatorAvailable,
				Status: configv1.ConditionTrue,
			},
			{
				Type:    configv1.OperatorProgressing,
				Status:  configv1.ConditionTrue,
				Reason:  reason,
				Message: message,
			},
			{
				Type:   configv1.OperatorDegraded,
				Status: configv1.ConditionFalse,
			},
		},
	}

	klog.Infof("Operator status progressing: %s", message)

	return r.ApplyStatus(status)
}

// Start checks the status of dependencies and reports the operator's status. It
// will poll until stopCh is closed or prerequisites are satisfied, in which
// case it will report the operator as available the configured version and wait
// for stopCh to close before returning.
func (r *StatusReporter) Start(c context.Context) error {
	interval := 15 * time.Second

	// Poll the status of our prerequisites and set our status
	// accordingly.  Rather than return errors and stop polling, most
	// errors here should just be reported in the status message.
	pollFunc := func(context.Context) (bool, error) {
		// TODO(jkyros): This doesn't handle the context yet, but someday it probably should
		return r.ReportStatus()
	}

	return wait.PollUntilContextCancel(c, interval, true, pollFunc)
}

// ReportStatus checks the status of each dependency and operand and reports the
// appropriate status via the operator's ClusterOperator object.
func (r *StatusReporter) ReportStatus() (bool, error) {
	// Check that any CluterAutoscaler deployments are updated and available.
	ok, err := r.CheckVPARecommender()
	if err != nil {
		msg := fmt.Sprintf("error checking VPA controllers status: %v", err)
		if err := r.degraded(ReasonCheckAutoscaler, msg); err != nil {
			return false, err
		}
		return false, nil
	}

	if !ok {
		msg := fmt.Sprintf("updating to %s", r.config.ReleaseVersion)
		if err := r.progressing(ReasonSyncing, msg); err != nil {
			return false, fmt.Errorf("failed to set progressing status: %v", err)
		}
		return false, nil
	}

	msg := fmt.Sprintf("at version %s", r.config.ReleaseVersion)
	if err := r.available(ReasonEmpty, msg); err != nil {
		return false, fmt.Errorf("failed to set available status: %v", err)
	}

	return true, nil
}

// CheckVPARecommender checks the status of any vpa-recommender
// deployments. It returns a bool indicating whether the deployments are
// available and fully updated to the latest version and an error.
func (r *StatusReporter) CheckVPARecommender() (bool, error) {
	vpa := &autoscalingv1.VerticalPodAutoscalerController{}
	caName := client.ObjectKey{Name: r.config.VerticalPodAutoscalerName, Namespace: r.config.VerticalPodAutoscalerNamespace}

	if err := r.client.Get(context.TODO(), caName, vpa); err != nil {
		if errors.IsNotFound(err) {
			klog.Info("No VerticalPodAutoscalerController. Reporting available.")
			return true, nil
		}

		klog.Errorf("Error getting VerticalPodAutoscalerController: %v", err)
		return false, err
	}

	deployment := &appsv1.Deployment{}
	deploymentName := client.ObjectKey{
		Name:      fmt.Sprintf("vpa-recommender-%s", r.config.VerticalPodAutoscalerName),
		Namespace: r.config.VerticalPodAutoscalerNamespace,
	}

	if err := r.client.Get(context.TODO(), deploymentName, deployment); err != nil {
		if errors.IsNotFound(err) {
			klog.Info("No vpa-recommender deployment. Reporting unavailable.")
			return false, nil
		}

		klog.Errorf("Error getting vpa-recommender deployment: %v", err)
		return false, err
	}

	if !util.ReleaseVersionMatches(deployment, r.config.ReleaseVersion) {
		klog.Info("vpa-recommender deployment version not current.")
		return false, nil
	}

	if !util.DeploymentUpdated(deployment) {
		klog.Info("vpa-recommender deployment updating.")
		return false, nil
	}

	klog.Info("vpa-recommender deployment is available and updated.")

	return true, nil
}
