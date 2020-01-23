package verticalpodautoscaler

import (
	"context"
	"fmt"

	autoscalingv1 "github.com/openshift/vertical-pod-autoscaler-operator/pkg/apis/autoscaling/v1"
	"github.com/openshift/vertical-pod-autoscaler-operator/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/reference"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName = "vertical-pod-autoscaler-controller"
	// Fraction of usage added as the safety margin to the recommended request. This default
	// matches the upstream default
	DefaultSafetyMarginFraction = 0.15
	// Minimum CPU recommendation for a pod. This default matches the upstream default
	DefaultPodMinCPUMillicores = 25
	// Minimum memory recommendation for a pod. This default matches the upstream default
	DefaultPodMinMemoryMb = 250
)

type ControllerParams struct {
	Command           string
	NameMethod        func(r *Reconciler, vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName
	ServiceAccount    string
	PriorityClassName string
	GetArgs           func(vpa *autoscalingv1.VerticalPodAutoscalerController, cfg *Config) []string
}

var controllerParams = [...]ControllerParams{
	{
		"recommender",
		(*Reconciler).RecommenderName,
		"vpa-recommender",
		"system-cluster-critical",
		RecommenderArgs,
	},
	{
		"updater",
		(*Reconciler).UpdaterName,
		"vpa-updater",
		"system-cluster-critical",
		UpdaterArgs,
	},
	{
		"admission-controller",
		(*Reconciler).AdmissionPluginName,
		"vpa-admission-controller",
		"system-cluster-critical",
		AdmissionPluginArgs,
	},
}

// NewReconciler returns a new Reconciler.
func NewReconciler(mgr manager.Manager, cfg *Config) *Reconciler {
	return &Reconciler{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		recorder: mgr.GetEventRecorderFor(controllerName),
		config:   cfg,
	}
}

// Config represents the configuration for a reconciler instance.
type Config struct {
	// The release version assigned to the operator config.
	ReleaseVersion string
	// The name of the singleton VerticalPodAutoscalerController resource.
	Name string
	// The namespace for vertical-pod-autoscaler deployments.
	Namespace string
	// The vertical-pod-autoscaler image to use in deployments.
	Image string
	// The log verbosity level for the vertical-pod-autoscaler.
	Verbosity int
	// Additional arguments passed to the vertical-pod-autoscaler.
	ExtraArgs string
}

var _ reconcile.Reconciler = &Reconciler{}

// Reconciler reconciles a VerticalPodAutoscalerController object
type Reconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	recorder record.EventRecorder
	scheme   *runtime.Scheme
	config   *Config
}

// AddToManager adds a new Controller to mgr with r as the reconcile.Reconciler
func (r *Reconciler) AddToManager(mgr manager.Manager) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// VerticalPodAutoscalerController is effectively a singleton resource.  A
	// deployment is only created if an instance is found matching the
	// name set at runtime.
	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return r.NamePredicate(e.Meta)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.NamePredicate(e.MetaNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return r.NamePredicate(e.Meta)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return r.NamePredicate(e.Meta)
		},
	}

	// Watch for changes to primary resource VerticalPodAutoscalerController
	err = c.Watch(&source.Kind{Type: &autoscalingv1.VerticalPodAutoscalerController{}}, &handler.EnqueueRequestForObject{}, p)
	if err != nil {
		return err
	}

	// Watch for changes to secondary resources owned by a VerticalPodAutoscalerController
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &autoscalingv1.VerticalPodAutoscalerController{},
	})

	if err != nil {
		return err
	}

	// Check to see if initial VPA instance exists, and if not, create it
	vpa := &autoscalingv1.VerticalPodAutoscalerController{}
	nn := types.NamespacedName{
		Name: r.config.Name,
	}
	err = r.client.Get(context.TODO(), nn, vpa)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("No VerticalPodAutoscalerController exists. Creating instance '%v'", nn)
			vpa = r.DefaultVPAController()
			// IsAlreadyExists is a harmless race, but any other error should be passed along
			if err = r.client.Create(context.TODO(), vpa); err != nil && !errors.IsAlreadyExists(err) {
				return err
			}
		} else {

			klog.Errorf("Error reading VerticalPodAutoscalerController: %v", err)
			return err
		}
	}
	return nil
}

// Reconcile reads that state of the cluster for a VerticalPodAutoscalerController
// object and makes changes based on the state read and what is in the
// VerticalPodAutoscalerController.Spec
func (r *Reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.Infof("Reconciling VerticalPodAutoscalerController %s\n", request.Name)

	// Fetch the VerticalPodAutoscalerController instance
	vpa := &autoscalingv1.VerticalPodAutoscalerController{}
	err := r.client.Get(context.TODO(), request.NamespacedName, vpa)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after
			// reconcile request.  Owned objects are automatically
			// garbage collected. For additional cleanup logic use
			// finalizers.  Return and don't requeue.
			klog.Infof("VerticalPodAutoscalerController %s not found, will not reconcile", request.Name)
			return reconcile.Result{}, nil
		}

		// Error reading the object - requeue the request.
		klog.Errorf("Error reading VerticalPodAutoscalerController: %v", err)
		return reconcile.Result{}, err
	}

	// vpaRef is a reference to the VerticalPodAutoscalerController object, but with the
	// namespace for vertical-pod-autoscaler deployments set.  This keeps events
	// generated for these cluster scoped objects out of the default namespace.
	vpaRef := r.objectReference(vpa)

	for _, params := range controllerParams {
		_, err = r.GetAutoscaler(vpa, params.NameMethod(r, vpa))
		if err != nil && !errors.IsNotFound(err) {
			errMsg := fmt.Sprintf("Error getting vertical-pod-autoscaler deployment: %v", err)
			r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedGetDeployment", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		}

		if errors.IsNotFound(err) {
			if err := r.CreateAutoscaler(vpa, params); err != nil {
				errMsg := fmt.Sprintf("Error creating VerticalPodAutoscalerController deployment: %v", err)
				r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedCreate", errMsg)
				klog.Error(errMsg)

				return reconcile.Result{}, err
			}

			msg := fmt.Sprintf("Created VerticalPodAutoscalerController deployment: %s", params.NameMethod(r, vpa))
			r.recorder.Eventf(vpaRef, corev1.EventTypeNormal, "SuccessfulCreate", msg)
			klog.Info(msg)
			continue
		}
		if err := r.UpdateAutoscaler(vpa, params); err != nil {
			errMsg := fmt.Sprintf("Error updating vertical-pod-autoscaler deployment: %v", err)
			r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedUpdate", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		}

		msg := fmt.Sprintf("Updated VerticalPodAutoscalerController deployment: %s", params.NameMethod(r, vpa))
		r.recorder.Eventf(vpaRef, corev1.EventTypeNormal, "SuccessfulUpdate", msg)
		klog.Info(msg)
	}

	return reconcile.Result{}, nil
}

// SetConfig sets the given config on the reconciler.
func (r *Reconciler) SetConfig(cfg *Config) {
	r.config = cfg
}

// NamePredicate is used in predicate functions.  It returns true if
// the object's name matches the configured name of the singleton
// VerticalPodAutoscalerController resource.
func (r *Reconciler) NamePredicate(meta metav1.Object) bool {
	// Only process events for objects matching the configured resource name.
	if meta.GetName() != r.config.Name {
		klog.Warningf("Not processing VerticalPodAutoscalerController %s, name must be %s", meta.GetName(), r.config.Name)
		return false
	}
	return true
}

// CreateAutoscaler will create the deployment for the given
// VerticalPodAutoscalerController custom resource instance.
func (r *Reconciler) CreateAutoscaler(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) error {
	klog.Infof("Creating VerticalPodAutoscalerController deployment: %s", params.NameMethod(r, vpa))
	deployment := r.AutoscalerDeployment(vpa, params)

	// Set VerticalPodAutoscalerController instance as the owner and controller.
	if err := controllerutil.SetControllerReference(vpa, deployment, r.scheme); err != nil {
		return err
	}

	return r.client.Create(context.TODO(), deployment)
}

// UpdateAutoscaler will retrieve the deployment for the given VerticalPodAutoscalerController
// custom resource instance and update it to match the expected spec if needed.
func (r *Reconciler) UpdateAutoscaler(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) error {

	existingDeployment, err := r.GetAutoscaler(vpa, params.NameMethod(r, vpa))
	if err != nil {
		return err
	}

	existingSpec := existingDeployment.Spec.Template.Spec
	expectedSpec := r.VPAPodSpec(vpa, params)

	// Only comparing podSpec and release version for now.
	if equality.Semantic.DeepEqual(existingSpec, expectedSpec) &&
		util.ReleaseVersionMatches(vpa, r.config.ReleaseVersion) {
		return nil
	}

	existingDeployment.Spec.Template.Spec = *expectedSpec

	r.UpdateAnnotations(existingDeployment)
	r.UpdateAnnotations(&existingDeployment.Spec.Template)

	return r.client.Update(context.TODO(), existingDeployment)
}

// GetAutoscaler will return the deployment for the given VerticalPodAutoscalerController
// custom resource instance.
func (r *Reconciler) GetAutoscaler(vpa *autoscalingv1.VerticalPodAutoscalerController, nn types.NamespacedName) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}

	if err := r.client.Get(context.TODO(), nn, deployment); err != nil {
		return nil, err
	}

	return deployment, nil
}

// RecommenderName returns the expected NamespacedName for the deployment
// belonging to the given VerticalPodAutoscalerController.
func (r *Reconciler) RecommenderName(vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName {
	return types.NamespacedName{
		Name:      fmt.Sprintf("vpa-recommender-%s", vpa.Name),
		Namespace: r.config.Namespace,
	}
}

// UpdaterName returns the expected NamespacedName for the deployment
// belonging to the given VerticalPodAutoscalerController.
func (r *Reconciler) UpdaterName(vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName {
	return types.NamespacedName{
		Name:      fmt.Sprintf("vpa-updater-%s", vpa.Name),
		Namespace: r.config.Namespace,
	}
}

// AdmissionPluginName returns the expected NamespacedName for the deployment
// belonging to the given VerticalPodAutoscalerController.
func (r *Reconciler) AdmissionPluginName(vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName {
	return types.NamespacedName{
		Name:      fmt.Sprintf("vpa-admission-plugin-%s", vpa.Name),
		Namespace: r.config.Namespace,
	}
}

// UpdateAnnotations updates the annotations on the given object to the values
// currently expected by the controller.
func (r *Reconciler) UpdateAnnotations(obj metav1.Object) {
	annotations := obj.GetAnnotations()

	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[util.CriticalPodAnnotation] = ""
	annotations[util.ReleaseVersionAnnotation] = r.config.ReleaseVersion

	obj.SetAnnotations(annotations)
}

// AutoscalerDeployment returns the expected deployment belonging to the given
// VerticalPodAutoscalerController.
func (r *Reconciler) AutoscalerDeployment(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *appsv1.Deployment {

	namespacedName := params.NameMethod(r, vpa)
	labels := map[string]string{
		"vertical-pod-autoscaler": vpa.Name,
		"app":                     "vertical-pod-autoscaler",
	}

	annotations := map[string]string{
		util.CriticalPodAnnotation:    "",
		util.ReleaseVersionAnnotation: r.config.ReleaseVersion,
	}

	podSpec := r.VPAPodSpec(vpa, params)
	replicas := int32(1)

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        namespacedName.Name,
			Namespace:   namespacedName.Namespace,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: *podSpec,
			},
		},
	}

	return deployment
}

// DefaultVPAController returns a default VerticalPodAutoscalerController instance
func (r *Reconciler) DefaultVPAController() *autoscalingv1.VerticalPodAutoscalerController {
	var smf float64 = DefaultSafetyMarginFraction
	var podcpu float64 = DefaultPodMinCPUMillicores
	var podminmem float64 = DefaultPodMinMemoryMb

	vpa := &autoscalingv1.VerticalPodAutoscalerController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.config.Name,
			Namespace: r.config.Namespace,
		},
	}
	vpa.Name = r.config.Name
	vpa.Spec.SafetyMarginFraction = &smf
	vpa.Spec.PodMinCPUMillicores = &podcpu
	vpa.Spec.PodMinMemoryMb = &podminmem
	return vpa
}

// VPAPodSpec returns the expected podSpec for the deployment belonging
// to the given VerticalPodAutoscalerController.
func (r *Reconciler) VPAPodSpec(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec {
	args := params.GetArgs(vpa, r.config)

	if r.config.ExtraArgs != "" {
		args = append(args, r.config.ExtraArgs)
	}

	spec := &corev1.PodSpec{
		ServiceAccountName: params.ServiceAccount,
		PriorityClassName:  params.PriorityClassName,
		NodeSelector: map[string]string{
			"node-role.kubernetes.io/master": "",
			"beta.kubernetes.io/os":          "linux",
		},
		Containers: []corev1.Container{
			{
				Name:    "vertical-pod-autoscaler",
				Image:   r.config.Image,
				Command: []string{params.Command},
				Args:    args,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName(corev1.ResourceCPU):    resource.MustParse("25m"),
						corev1.ResourceName(corev1.ResourceMemory): resource.MustParse("25Mi"),
					},
				},
			},
		},
		Tolerations: []corev1.Toleration{
			{
				Key:      "CriticalAddonsOnly",
				Operator: corev1.TolerationOpExists,
			},
			{

				Key:      "node-role.kubernetes.io/master",
				Effect:   corev1.TaintEffectNoSchedule,
				Operator: corev1.TolerationOpExists,
			},
		},
	}

	return spec
}

// objectReference returns a reference to the given object, but will set the
// configured deployment namesapce if no namespace was previously set.  This is
// useful for referencing cluster scoped objects in events without the events
// being created in the default namespace.
func (r *Reconciler) objectReference(obj runtime.Object) *corev1.ObjectReference {
	ref, err := reference.GetReference(r.scheme, obj)
	if err != nil {
		klog.Errorf("Error creating object reference: %v", err)
		return nil
	}

	if ref != nil && ref.Namespace == "" {
		ref.Namespace = r.config.Namespace
	}

	return ref
}
