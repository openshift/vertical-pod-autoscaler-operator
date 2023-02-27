package verticalpodautoscaler

import (
	"context"
	"fmt"
	"time"

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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/reference"
	"k8s.io/klog"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/cache"
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
	ControllerName            = "vertical-pod-autoscaler-controller"
	WebhookServiceName        = "vpa-webhook"
	WebhookCertSecretName     = "vpa-tls-certs"
	WebhookCertAnnotationName = "service.beta.openshift.io/serving-cert-secret-name"
	CACertAnnotationName      = "service.beta.openshift.io/inject-cabundle"
	CACertConfigMapName       = "vpa-tls-ca-certs"

	AdmissionControllerAppName = "vpa-admission-controller"
	// Fraction of usage added as the safety margin to the recommended request. This default
	// matches the upstream default
	DefaultSafetyMarginFraction = 0.15
	// Minimum CPU recommendation for a pod. This default matches the upstream default
	DefaultPodMinCPUMillicores = 25
	// Minimum memory recommendation for a pod. This default matches the upstream default
	DefaultPodMinMemoryMb = 250
	// By default, the VPA will not run in recommendation-only mode. The Updater and Admission plugin will run
	DefaultRecommendationOnly = false
	// By default, the updater will not kill pods if they are the only replica
	DefaultMinReplicas = 2
)

type ControllerParams struct {
	Command           string
	NameMethod        func(r *Reconciler, vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName
	AppName           string
	ServiceAccount    string
	PriorityClassName string
	GetArgs           func(vpa *autoscalingv1.VerticalPodAutoscalerController, cfg *Config) []string
	EnabledMethod     func(r *Reconciler, vpa *autoscalingv1.VerticalPodAutoscalerController) bool
	PodSpecMethod     func(r *Reconciler, vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec
}

var controllerParams = [...]ControllerParams{
	{
		"recommender",
		(*Reconciler).RecommenderName,
		"vpa-recommender",
		"vpa-recommender",
		"system-cluster-critical",
		RecommenderArgs,
		(*Reconciler).RecommenderEnabled,
		(*Reconciler).VPAPodSpec,
	},
	{
		"updater",
		(*Reconciler).UpdaterName,
		"vpa-updater",
		"vpa-updater",
		"system-cluster-critical",
		UpdaterArgs,
		(*Reconciler).UpdaterEnabled,
		(*Reconciler).VPAPodSpec,
	},
	{
		"admission-controller",
		(*Reconciler).AdmissionPluginName,
		AdmissionControllerAppName,
		"vpa-admission-controller",
		"system-cluster-critical",
		AdmissionPluginArgs,
		(*Reconciler).AdmissionPluginEnabled,
		(*Reconciler).AdmissionControllerPodSpec,
	},
}

// NewReconciler returns a new Reconciler.
func NewReconciler(mgr manager.Manager, cfg *Config) *Reconciler {
	return &Reconciler{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		recorder: mgr.GetEventRecorderFor(ControllerName),
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
	c, err := controller.New(ControllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// VerticalPodAutoscalerController is effectively a singleton resource.  A
	// deployment is only created if an instance is found matching the
	// name set at runtime.
	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return r.NamePredicate(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.NamePredicate(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return r.NamePredicate(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return r.NamePredicate(e.Object)
		},
	}

	// Watch for changes to primary resource VerticalPodAutoscalerController
	err = c.Watch(&source.Kind{Type: &autoscalingv1.VerticalPodAutoscalerController{}}, &handler.EnqueueRequestForObject{}, p)
	if err != nil {
		return err
	}

	// Watch for changes to secondary resources owned by a VerticalPodAutoscalerController
	objTypes := []client.Object{
		&appsv1.Deployment{},
		&corev1.Service{},
		&corev1.ConfigMap{},
	}
	for _, objType := range objTypes {
		err = c.Watch(&source.Kind{Type: objType}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &autoscalingv1.VerticalPodAutoscalerController{},
		})

		if err != nil {
			return err
		}
	}

	go func() {
		// Check to see if initial VPA instance exists, and if not, create it
		vpa := &autoscalingv1.VerticalPodAutoscalerController{}
		nn := types.NamespacedName{
			Name: r.config.Name,
		}
		for i := 0; i < 60; i++ {
			time.Sleep(1 * time.Second)
			err = r.client.Get(context.TODO(), nn, vpa)
			if err == nil { // instance already exists, no need to create a default instance
				return
			}
			if _, ok := err.(*cache.ErrCacheNotStarted); ok {
				klog.Info("Waiting for manager to start before checking to see if a VerticalPodAutoscalerController instance exists")
			} else if errors.IsNotFound(err) {
				klog.Infof("No VerticalPodAutoscalerController exists. Creating instance '%v'", nn)
				vpa = r.DefaultVPAController()
				// IsAlreadyExists is a harmless race, but any other error should be logged
				if err = r.client.Create(context.TODO(), vpa); err != nil && !errors.IsAlreadyExists(err) {
					klog.Errorf("Error creating default VerticalPodAutoscalerController instance: %v", err)
				}
				return
			} else {
				klog.Errorf("Error reading VerticalPodAutoscalerController: %v", err)
				return
			}
		}
		klog.Errorf("Unable to create default VerticalPodAutoscalerController instance: timed out waiting for manager to start")
	}()

	return nil
}

// Reconcile reads that state of the cluster for a VerticalPodAutoscalerController
// object and makes changes based on the state read and what is in the
// VerticalPodAutoscalerController.Spec
func (r *Reconciler) Reconcile(c context.Context, request reconcile.Request) (reconcile.Result, error) {
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
		deployment := &appsv1.Deployment{}
		err := r.client.Get(context.TODO(), params.NameMethod(r, vpa), deployment)
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
		if updated, err := r.UpdateAutoscaler(vpa, params); err != nil {
			errMsg := fmt.Sprintf("Error updating vertical-pod-autoscaler deployment: %v", err)
			r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedUpdate", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		} else if updated {
			msg := fmt.Sprintf("Updated VerticalPodAutoscalerController deployment: %s", params.NameMethod(r, vpa))
			r.recorder.Eventf(vpaRef, corev1.EventTypeNormal, "SuccessfulUpdate", msg)
			klog.Info(msg)
		}
	}

	whnn := types.NamespacedName{
		Name:      WebhookServiceName,
		Namespace: r.config.Namespace,
	}

	service := &corev1.Service{}
	err = r.client.Get(context.TODO(), whnn, service)
	if err != nil && !errors.IsNotFound(err) {
		errMsg := fmt.Sprintf("Error getting vertical-pod-autoscaler webhook service %v: %v", WebhookServiceName, err)
		r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedGetService", errMsg)
		klog.Error(errMsg)

		return reconcile.Result{}, err
	}

	if errors.IsNotFound(err) {
		if err := r.CreateWebhookService(vpa); err != nil {
			errMsg := fmt.Sprintf("Error creating VerticalPodAutoscalerController service: %v", err)
			r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedCreate", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		}

		msg := fmt.Sprintf("Created VerticalPodAutoscalerController service: %s", WebhookServiceName)
		r.recorder.Eventf(vpaRef, corev1.EventTypeNormal, "SuccessfulCreate", msg)
		klog.Info(msg)
	} else {
		if updated, err := r.UpdateWebhookService(vpa); err != nil {
			errMsg := fmt.Sprintf("Error updating vertical-pod-autoscaler webhook service: %v", err)
			r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedUpdate", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		} else if updated {
			msg := fmt.Sprintf("Updated VerticalPodAutoscalerController service: %s", WebhookServiceName)
			r.recorder.Eventf(vpaRef, corev1.EventTypeNormal, "SuccessfulUpdate", msg)
			klog.Info(msg)
		}
	}

	cmnn := types.NamespacedName{
		Name:      CACertConfigMapName,
		Namespace: r.config.Namespace,
	}
	cm := &corev1.ConfigMap{}
	err = r.client.Get(context.TODO(), cmnn, cm)
	if err != nil && !errors.IsNotFound(err) {
		errMsg := fmt.Sprintf("Error getting vertical-pod-autoscaler CA ConfigMap %v: %v", CACertConfigMapName, err)
		r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedGetConfigMap", errMsg)
		klog.Error(errMsg)

		return reconcile.Result{}, err
	}

	if errors.IsNotFound(err) {
		if err := r.CreateCAConfigMap(vpa); err != nil {
			errMsg := fmt.Sprintf("Error creating VerticalPodAutoscalerController ConfigMap: %v", err)
			r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedCreate", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		}

		msg := fmt.Sprintf("Created VerticalPodAutoscalerController ConfigMap: %s", CACertConfigMapName)
		r.recorder.Eventf(vpaRef, corev1.EventTypeNormal, "SuccessfulCreate", msg)
		klog.Info(msg)
	} else {
		if updated, err := r.UpdateCAConfigMap(vpa); err != nil {
			errMsg := fmt.Sprintf("Error updating vertical-pod-autoscaler CA ConfigMap: %v", err)
			r.recorder.Event(vpaRef, corev1.EventTypeWarning, "FailedUpdate", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		} else if updated {
			msg := fmt.Sprintf("Updated VerticalPodAutoscalerController ConfigMap: %s", CACertConfigMapName)
			r.recorder.Eventf(vpaRef, corev1.EventTypeNormal, "SuccessfulUpdate", msg)
			klog.Info(msg)
		}
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
func (r *Reconciler) UpdateAutoscaler(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) (updated bool, err error) {
	existingDeployment := &appsv1.Deployment{}
	err = r.client.Get(context.TODO(), params.NameMethod(r, vpa), existingDeployment)
	if err != nil {
		return false, err
	}

	existingSpec := &existingDeployment.Spec.Template.Spec
	expectedSpec := params.PodSpecMethod(r, vpa, params)
	expectedReplicas := int32(1)
	// disable the controller if it shouldn't be enabled
	if !params.EnabledMethod(r, vpa) {
		expectedReplicas = 0
	}

	// Only comparing podSpec, replicas and release version for now.
	if equality.Semantic.DeepEqual(existingSpec, expectedSpec) &&
		equality.Semantic.DeepEqual(existingDeployment.Spec.Replicas, &expectedReplicas) &&
		util.ReleaseVersionMatches(existingDeployment, r.config.ReleaseVersion) {
		return false, err
	}

	existingDeployment.Spec.Template.Spec = *expectedSpec
	existingDeployment.Spec.Replicas = &expectedReplicas

	r.UpdateAnnotations(existingDeployment)
	r.UpdateAnnotations(&existingDeployment.Spec.Template)
	err = r.client.Update(context.TODO(), existingDeployment)
	return err == nil, err
}

// CreateWebhookService will create the webhook service for the given
// VerticalPodAutoscalerController custom resource instance.
func (r *Reconciler) CreateWebhookService(vpa *autoscalingv1.VerticalPodAutoscalerController) error {
	klog.Infof("Creating VerticalPodAutoscalerController service: %s", WebhookServiceName)
	service := r.WebHookService(vpa)

	// Set VerticalPodAutoscalerController instance as the owner and controller.
	if err := controllerutil.SetControllerReference(vpa, service, r.scheme); err != nil {
		return err
	}

	return r.client.Create(context.TODO(), service)
}

// UpdateWebhookService will retrieve the service for the given VerticalPodAutoscalerController
// custom resource instance and update it to match the expected spec if needed.
func (r *Reconciler) UpdateWebhookService(vpa *autoscalingv1.VerticalPodAutoscalerController) (updated bool, err error) {

	nn := types.NamespacedName{
		Name:      WebhookServiceName,
		Namespace: r.config.Namespace,
	}
	existingService := &corev1.Service{}
	err = r.client.Get(context.TODO(), nn, existingService)
	if err != nil {
		return false, err
	}

	merged := existingService.DeepCopy()
	expected := r.WebHookService(vpa)
	// Only comparing service spec.ports, spec.selector, and annotations (including release version)
	merged.Spec.Ports = expected.Spec.Ports
	merged.Spec.Selector = expected.Spec.Selector
	r.UpdateServiceAnnotations(merged)
	if equality.Semantic.DeepEqual(existingService, merged) {
		return false, nil
	}

	err = r.client.Update(context.TODO(), merged)
	return err == nil, err
}

// CreateCAConfigMap will create the CA ConfigMap for the given
// VerticalPodAutoscalerController custom resource instance.
func (r *Reconciler) CreateCAConfigMap(vpa *autoscalingv1.VerticalPodAutoscalerController) error {
	klog.Infof("Creating VerticalPodAutoscalerController configmap: %s", CACertConfigMapName)
	cm := r.CAConfigMap(vpa)

	// Set VerticalPodAutoscalerController instance as the owner and controller.
	if err := controllerutil.SetControllerReference(vpa, cm, r.scheme); err != nil {
		return err
	}

	return r.client.Create(context.TODO(), cm)
}

// UpdateCAConfigMap will retrieve the CA ConfigMap for the given VerticalPodAutoscalerController
// custom resource instance and update it to match the expected spec if needed.
func (r *Reconciler) UpdateCAConfigMap(vpa *autoscalingv1.VerticalPodAutoscalerController) (updated bool, err error) {

	nn := types.NamespacedName{
		Name:      CACertConfigMapName,
		Namespace: r.config.Namespace,
	}
	existingCM := &corev1.ConfigMap{}
	err = r.client.Get(context.TODO(), nn, existingCM)
	if err != nil {
		return false, err
	}

	merged := existingCM.DeepCopy()
	// Only comparing annotations (including release version)
	r.UpdateConfigMapAnnotations(merged)
	if equality.Semantic.DeepEqual(existingCM, merged) {
		return false, nil
	}
	err = r.client.Update(context.TODO(), merged)
	return err == nil, err
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

// RecommenderEnabled returns true if the recommender should be enabled
func (r *Reconciler) RecommenderEnabled(vpa *autoscalingv1.VerticalPodAutoscalerController) bool {
	return true
}

// UpdaterEnabled returns true if the recommender should be enabled
func (r *Reconciler) UpdaterEnabled(vpa *autoscalingv1.VerticalPodAutoscalerController) bool {
	return vpa.Spec.RecommendationOnly == nil || *vpa.Spec.RecommendationOnly == false
}

// AdmissionPluginEnabled returns true if the recommender should be enabled
func (r *Reconciler) AdmissionPluginEnabled(vpa *autoscalingv1.VerticalPodAutoscalerController) bool {
	return vpa.Spec.RecommendationOnly == nil || *vpa.Spec.RecommendationOnly == false
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

// UpdateServiceAnnotations updates the annotations on the given object to the values
// currently expected by the controller.
func (r *Reconciler) UpdateServiceAnnotations(obj metav1.Object) {
	annotations := obj.GetAnnotations()

	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[util.ReleaseVersionAnnotation] = r.config.ReleaseVersion
	annotations[WebhookCertAnnotationName] = WebhookCertSecretName

	obj.SetAnnotations(annotations)
}

// UpdateConfigMapAnnotations updates the annotations on the given object to the values
// currently expected by the controller.
func (r *Reconciler) UpdateConfigMapAnnotations(obj metav1.Object) {
	annotations := obj.GetAnnotations()

	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[util.ReleaseVersionAnnotation] = r.config.ReleaseVersion
	annotations[CACertAnnotationName] = "true"

	obj.SetAnnotations(annotations)
}

// AutoscalerDeployment returns the expected deployment belonging to the given
// VerticalPodAutoscalerController.
func (r *Reconciler) AutoscalerDeployment(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *appsv1.Deployment {

	namespacedName := params.NameMethod(r, vpa)
	labels := map[string]string{
		"vertical-pod-autoscaler": vpa.Name,
		"app":                     params.AppName,
	}

	annotations := map[string]string{
		util.CriticalPodAnnotation:    "",
		util.ReleaseVersionAnnotation: r.config.ReleaseVersion,
	}

	podSpec := params.PodSpecMethod(r, vpa, params)
	replicas := int32(1)
	// disable the controller if it shouldn't be enabled
	if !params.EnabledMethod(r, vpa) {
		replicas = 0
	}

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
	var recommendationOnly bool = DefaultRecommendationOnly
	var minReplicas int64 = DefaultMinReplicas

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
	vpa.Spec.RecommendationOnly = &recommendationOnly
	vpa.Spec.MinReplicas = &minReplicas
	return vpa
}

// VPAPodSpec returns the expected podSpec for the deployment belonging
// to the given VerticalPodAutoscalerController.
func (r *Reconciler) VPAPodSpec(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec {
	args := params.GetArgs(vpa, r.config)

	if r.config.ExtraArgs != "" {
		args = append(args, r.config.ExtraArgs)
	}
	gracePeriod := int64(30)

	spec := &corev1.PodSpec{
		ServiceAccountName:       params.ServiceAccount,
		DeprecatedServiceAccount: params.ServiceAccount,
		PriorityClassName:        params.PriorityClassName,
		NodeSelector: map[string]string{
			"node-role.kubernetes.io/master": "",
			"beta.kubernetes.io/os":          "linux",
		},
		Containers: []corev1.Container{
			{
				Name:            "vertical-pod-autoscaler",
				Image:           r.config.Image,
				ImagePullPolicy: "Always",
				Command:         []string{params.Command},
				Args:            args,
				Env: []corev1.EnvVar{
					{
						Name: "NAMESPACE",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								APIVersion: "v1",
								FieldPath:  "metadata.namespace",
							},
						},
					},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName(corev1.ResourceCPU):    resource.MustParse("25m"),
						corev1.ResourceName(corev1.ResourceMemory): resource.MustParse("25Mi"),
					},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: pointer.BoolPtr(false),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
					RunAsNonRoot: pointer.BoolPtr(true),
					SeccompProfile: &corev1.SeccompProfile{
						Type: "RuntimeDefault",
					},
				},
				TerminationMessagePath:   "/dev/termination-log",
				TerminationMessagePolicy: "File",
			},
		},
		DNSPolicy:                     corev1.DNSClusterFirst,
		RestartPolicy:                 corev1.RestartPolicyAlways,
		TerminationGracePeriodSeconds: &gracePeriod,
		SchedulerName:                 "default-scheduler",
		SecurityContext:               &corev1.PodSecurityContext{},
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

// AdmissionControllerPodSpec returns the expected podSpec for the Admission Controller deployment belonging
// to the given VerticalPodAutoscalerController.
func (r *Reconciler) AdmissionControllerPodSpec(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec {
	spec := r.VPAPodSpec(vpa, params)
	spec.Containers[0].Ports = append(spec.Containers[0].Ports, corev1.ContainerPort{
		ContainerPort: 8000,
		Protocol:      corev1.ProtocolTCP,
	})
	spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      "tls-certs",
		MountPath: "/data/tls-certs",
		ReadOnly:  true,
	})
	spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      "tls-ca-certs",
		MountPath: "/data/tls-ca-certs",
		ReadOnly:  true,
	})
	defaultMode := int32(0644)
	spec.Volumes = append(spec.Volumes, corev1.Volume{
		Name: "tls-certs",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  WebhookCertSecretName,
				DefaultMode: &defaultMode,
			},
		},
	})
	spec.Volumes = append(spec.Volumes, corev1.Volume{
		Name: "tls-ca-certs",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: CACertConfigMapName,
				},
				DefaultMode: &defaultMode,
			},
		},
	})
	return spec
}

// WebHookService returns the expected service belonging to the given
// VerticalPodAutoscalerController.
func (r *Reconciler) WebHookService(vpa *autoscalingv1.VerticalPodAutoscalerController) *corev1.Service {
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      WebhookServiceName,
			Namespace: r.config.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       443,
					TargetPort: intstr.FromInt(8000),
					Protocol:   "TCP",
				},
			},
			Selector: map[string]string{
				"app": AdmissionControllerAppName,
			},
		},
	}

	r.UpdateServiceAnnotations(service)
	return service
}

// CAConfigMap returns the expected CA ConfigMap belonging to the given
// VerticalPodAutoscalerController.
func (r *Reconciler) CAConfigMap(vpa *autoscalingv1.VerticalPodAutoscalerController) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CACertConfigMapName,
			Namespace: r.config.Namespace,
		},
	}

	r.UpdateConfigMapAnnotations(cm)
	return cm
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
