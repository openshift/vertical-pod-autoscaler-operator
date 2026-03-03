/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package verticalpodautoscaler

import (
	"context"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/reference"
	"k8s.io/klog"
	"k8s.io/utils/ptr"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	autoscalingv1 "github.com/openshift/vertical-pod-autoscaler-operator/api/v1"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/util"
)

const (
	// ControllerName The hard-coded name of the VPA controller
	ControllerName = "vertical-pod-autoscaler-controller"
	// WebhookServiceName The hard-coded name of the VPA webhook
	WebhookServiceName = "vpa-webhook"
	// WebhookCertSecretName The hard-coded name of the secret containing the VPA webhook's TLS cert
	WebhookCertSecretName     = "vpa-tls-certs"
	webhookCertAnnotationName = "service.beta.openshift.io/serving-cert-secret-name"
	cACertAnnotationName      = "service.beta.openshift.io/inject-cabundle"
	// CACertConfigMapName The hard-coded name of the configmap containing the CA certs for the VPA webhook
	CACertConfigMapName = "vpa-tls-ca-certs"

	// AdmissionControllerAppName The hard-coded name of the VPA admission controller
	AdmissionControllerAppName = "vpa-admission-controller"
	// DefaultSafetyMarginFraction Fraction of usage added as the safety margin to the recommended request. This default
	// matches the upstream default
	DefaultSafetyMarginFraction = float64(0.15)
	// DefaultPodMinCPUMillicores Minimum CPU recommendation for a pod. This default matches the upstream default
	DefaultPodMinCPUMillicores = float64(25)
	// DefaultPodMinMemoryMb Minimum memory recommendation for a pod. This default matches the upstream default
	DefaultPodMinMemoryMb = float64(250)
	// DefaultRecommendationOnly By default, the VPA will not run in recommendation-only mode. The Updater and Admission plugin will run
	DefaultRecommendationOnly = false
	// DefaultMinReplicas By default, the updater will not kill pods if they are the only replica
	DefaultMinReplicas   = int64(2)
	AdmissionWebhookPort = int16(8000)
)

// Default request CPU and memory for the VPA operands
var (
	AdmissionResourceRequirements = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("25m"),
			corev1.ResourceMemory: resource.MustParse("50Mi"),
		},
	}
	RecommenderResourceRequirements = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("25m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		},
	}
	UpdaterResourceRequirements = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("25m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		},
	}
	// No default limits, since there might be OOM killing or throttling if using in large cluster
)

// ControllerParams Parameters for running each of the 3 VPA operands
type ControllerParams struct {
	Command              string
	NameMethod           func(r *VerticalPodAutoscalerControllerReconciler, vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName
	AppName              string
	ServiceAccount       string
	PriorityClassName    string
	GetArgs              func(vpa *autoscalingv1.VerticalPodAutoscalerController, cfg *Config) []string
	EnabledMethod        func(r *VerticalPodAutoscalerControllerReconciler, vpa *autoscalingv1.VerticalPodAutoscalerController) bool
	PodSpecMethod        func(r *VerticalPodAutoscalerControllerReconciler, vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec
	ResourceRequirements corev1.ResourceRequirements
}

var controllerParams = [...]ControllerParams{
	{
		"recommender",
		(*VerticalPodAutoscalerControllerReconciler).RecommenderName,
		"vpa-recommender",
		"vpa-recommender",
		"system-cluster-critical",
		RecommenderArgs,
		(*VerticalPodAutoscalerControllerReconciler).RecommenderEnabled,
		(*VerticalPodAutoscalerControllerReconciler).RecommenderControllerPodSpec,
		RecommenderResourceRequirements,
	},
	{
		"updater",
		(*VerticalPodAutoscalerControllerReconciler).UpdaterName,
		"vpa-updater",
		"vpa-updater",
		"system-cluster-critical",
		UpdaterArgs,
		(*VerticalPodAutoscalerControllerReconciler).UpdaterEnabled,
		(*VerticalPodAutoscalerControllerReconciler).UpdaterControllerPodSpec,
		UpdaterResourceRequirements,
	},
	{
		"admission-controller",
		(*VerticalPodAutoscalerControllerReconciler).AdmissionPluginName,
		AdmissionControllerAppName,
		"vpa-admission-controller",
		"system-cluster-critical",
		AdmissionPluginArgs,
		(*VerticalPodAutoscalerControllerReconciler).AdmissionPluginEnabled,
		(*VerticalPodAutoscalerControllerReconciler).AdmissionControllerPodSpec,
		AdmissionResourceRequirements,
	},
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

// VerticalPodAutoscalerControllerReconciler reconciles a VerticalPodAutoscalerController object
type VerticalPodAutoscalerControllerReconciler struct {
	client.Client
	Cache    cache.Cache
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder events.EventRecorder
	Config   *Config
}

// +kubebuilder:rbac:groups=autoscaling.openshift.io,resources=verticalpodautoscalercontrollers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.openshift.io,resources=verticalpodautoscalercontrollers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.openshift.io,resources=verticalpodautoscalercontrollers/finalizers,verbs=update
// +kubebuilder:rbac:groups=autoscaling.openshift.io,resources=*,verbs=*
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=*
// +kubebuilder:rbac:groups="",resources=pods;events;configmaps;services;secrets,verbs=*
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
func (r *VerticalPodAutoscalerControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling VerticalPodAutoscalerController")

	// Fetch the VerticalPodAutoscalerController instance
	vpa := &autoscalingv1.VerticalPodAutoscalerController{}
	err := r.Get(context.TODO(), req.NamespacedName, vpa)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after
			// reconcile request.  Owned objects are automatically
			// garbage collected. For additional cleanup logic use
			// finalizers.  Return and don't requeue.
			reqLogger.Info("VerticalPodAutoscalerController not found, will not reconcile")
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
		err := r.Get(context.TODO(), params.NameMethod(r, vpa), deployment)
		if err != nil && !errors.IsNotFound(err) {
			errMsg := fmt.Sprintf("Error getting vertical-pod-autoscaler deployment: %v", err)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedGetDeployment", "", "%s", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		}

		if errors.IsNotFound(err) {
			if err := r.CreateAutoscaler(vpa, params); err != nil {
				errMsg := fmt.Sprintf("Error creating VerticalPodAutoscalerController deployment: %v", err)
				r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedCreate", "", "%s", errMsg)
				klog.Error(errMsg)

				return reconcile.Result{}, err
			}

			msg := fmt.Sprintf("Created VerticalPodAutoscalerController deployment: %s", params.NameMethod(r, vpa))
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeNormal, "SuccessfulCreate", "", "%s", msg)
			klog.Info(msg)
			continue
		}
		if updated, err := r.UpdateAutoscaler(vpa, params); err != nil {
			errMsg := fmt.Sprintf("Error updating vertical-pod-autoscaler deployment: %v", err)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedUpdate", "", "%s", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		} else if updated {
			msg := fmt.Sprintf("Updated VerticalPodAutoscalerController deployment: %s", params.NameMethod(r, vpa))
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeNormal, "SuccessfulUpdate", "", "%s", msg)
			klog.Info(msg)
		}
	}

	whnn := types.NamespacedName{
		Name:      WebhookServiceName,
		Namespace: r.Config.Namespace,
	}

	service := &corev1.Service{}
	err = r.Get(context.TODO(), whnn, service)
	if err != nil && !errors.IsNotFound(err) {
		errMsg := fmt.Sprintf("Error getting vertical-pod-autoscaler webhook service %v: %v", WebhookServiceName, err)
		r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedGetService", "", "%s", errMsg)
		klog.Error(errMsg)

		return reconcile.Result{}, err
	}

	if errors.IsNotFound(err) {
		if err := r.CreateWebhookService(vpa); err != nil {
			errMsg := fmt.Sprintf("Error creating VerticalPodAutoscalerController service: %v", err)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedCreate", "", "%s", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		}

		msg := fmt.Sprintf("Created VerticalPodAutoscalerController service: %s", WebhookServiceName)
		r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeNormal, "SuccessfulCreate", "", "%s", msg)
		klog.Info(msg)
	} else {
		if updated, err := r.UpdateWebhookService(vpa); err != nil {
			errMsg := fmt.Sprintf("Error updating vertical-pod-autoscaler webhook service: %v", err)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedUpdate", "", "%s", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		} else if updated {
			msg := fmt.Sprintf("Updated VerticalPodAutoscalerController service: %s", WebhookServiceName)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeNormal, "SuccessfulUpdate", "", "%s", msg)
			klog.Info(msg)
		}
	}

	cmnn := types.NamespacedName{
		Name:      CACertConfigMapName,
		Namespace: r.Config.Namespace,
	}
	cm := &corev1.ConfigMap{}
	err = r.Get(context.TODO(), cmnn, cm)
	if err != nil && !errors.IsNotFound(err) {
		errMsg := fmt.Sprintf("Error getting vertical-pod-autoscaler CA ConfigMap %v: %v", CACertConfigMapName, err)
		r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedGetConfigMap", "", "%s", errMsg)
		klog.Error(errMsg)

		return reconcile.Result{}, err
	}

	if errors.IsNotFound(err) {
		if err := r.CreateCAConfigMap(vpa); err != nil {
			errMsg := fmt.Sprintf("Error creating VerticalPodAutoscalerController ConfigMap: %v", err)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedCreate", "", "%s", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		}

		msg := fmt.Sprintf("Created VerticalPodAutoscalerController ConfigMap: %s", CACertConfigMapName)
		r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeNormal, "SuccessfulCreate", "", "%s", msg)
		klog.Info(msg)
	} else {
		if updated, err := r.UpdateCAConfigMap(vpa); err != nil {
			errMsg := fmt.Sprintf("Error updating vertical-pod-autoscaler CA ConfigMap: %v", err)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedUpdate", "", "%s", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		} else if updated {
			msg := fmt.Sprintf("Updated VerticalPodAutoscalerController ConfigMap: %s", CACertConfigMapName)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeNormal, "SuccessfulUpdate", "", "%s", msg)
			klog.Info(msg)
		}
	}

	for _, policy := range r.NetworkPolicies(vpa) {
		oldpolicy := &networkingv1.NetworkPolicy{}
		err = r.Get(context.TODO(), types.NamespacedName{Name: policy.Name, Namespace: r.Config.Namespace}, oldpolicy)
		if err != nil && !errors.IsNotFound(err) {
			errMsg := fmt.Sprintf("Error getting VerticalPodAutoscalerController networkpolicy %v: %v", policy.Name, err)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedGetNetworkPolicy", "", "%s", errMsg)
			klog.Error(errMsg)

			return reconcile.Result{}, err
		}

		if errors.IsNotFound(err) {

			// Set VerticalPodAutoscalerController instance as the owner and controller.
			if err := controllerutil.SetControllerReference(vpa, &policy, r.Scheme); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.Create(context.TODO(), &policy); err != nil {
				errMsg := fmt.Sprintf("Error creating VerticalPodAutoscalerController networkpolicy %v: %v", policy.Name, err)
				r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedCreate", "", "%s", errMsg)
				klog.Error(errMsg)

				return reconcile.Result{}, err
			}

			msg := fmt.Sprintf("Created VerticalPodAutoscalerController networkpolicy: %s", policy.Name)
			r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeNormal, "SuccessfulCreate", "", "%s", msg)
			klog.Info(msg)
		} else {
			if equality.Semantic.DeepEqual(policy.Spec, oldpolicy.Spec) {
				continue
			}
			if err := r.Update(context.TODO(), &policy); err != nil {
				errMsg := fmt.Sprintf("Error updating VerticalPodAutoscalerController networkpolicy %s: %v", policy.Name, err)
				r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeWarning, "FailedUpdate", "", "%s", errMsg)
				klog.Error(errMsg)

				return reconcile.Result{}, err
			} else {
				msg := fmt.Sprintf("Updated VerticalPodAutoscalerController networkpolicy: %s", policy.Name)
				r.Recorder.Eventf(vpaRef, nil, corev1.EventTypeNormal, "SuccessfulUpdate", "", "%s", msg)
				klog.Info(msg)
			}
		}
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VerticalPodAutoscalerControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	go func() {
		// Check to see if initial VPA instance exists, and if not, create it
		vpa := &autoscalingv1.VerticalPodAutoscalerController{}
		nn := types.NamespacedName{
			Name:      r.Config.Name,
			Namespace: r.Config.Namespace,
		}
		for i := 0; i < 60; i++ {
			time.Sleep(1 * time.Second)
			err = r.Get(context.TODO(), nn, vpa)
			if err == nil { // instance already exists, no need to create a default instance
				return
			}
			if _, ok := err.(*cache.ErrCacheNotStarted); ok {
				klog.Info("Waiting for manager to start before checking to see if a VerticalPodAutoscalerController instance exists")
			} else if errors.IsNotFound(err) {
				klog.Infof("No VerticalPodAutoscalerController exists. Creating instance '%v'", nn)
				vpa = r.DefaultVPAController()
				// IsAlreadyExists is a harmless race, but any other error should be logged
				if err = r.Create(context.TODO(), vpa); err != nil && !errors.IsAlreadyExists(err) {
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

	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.VerticalPodAutoscalerController{}, builder.WithPredicates(predicate.Funcs{
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
		})).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

// SetConfig sets the given config on the reconciler.
func (r *VerticalPodAutoscalerControllerReconciler) SetConfig(cfg *Config) {
	r.Config = cfg
}

// NamePredicate is used in predicate functions.  It returns true if
// the object's name matches the configured name of the singleton
// VerticalPodAutoscalerController resource.
func (r *VerticalPodAutoscalerControllerReconciler) NamePredicate(meta metav1.Object) bool {
	// Only process events for objects matching the configured resource name.
	if meta.GetName() != r.Config.Name {
		klog.Warningf("Not processing VerticalPodAutoscalerController %s, name must be %s", meta.GetName(), r.Config.Name)
		return false
	}
	return true
}

// CreateAutoscaler will create the deployment for the given
// VerticalPodAutoscalerController custom resource instance.
func (r *VerticalPodAutoscalerControllerReconciler) CreateAutoscaler(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) error {
	klog.Infof("Creating VerticalPodAutoscalerController deployment: %s", params.NameMethod(r, vpa))
	deployment := r.AutoscalerDeployment(vpa, params)

	// Set VerticalPodAutoscalerController instance as the owner and controller.
	if err := controllerutil.SetControllerReference(vpa, deployment, r.Scheme); err != nil {
		return err
	}

	return r.Create(context.TODO(), deployment)
}

// UpdateAutoscaler will retrieve the deployment for the given VerticalPodAutoscalerController
// custom resource instance and update it to match the expected spec if needed.
func (r *VerticalPodAutoscalerControllerReconciler) UpdateAutoscaler(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) (updated bool, err error) {
	existingDeployment := &appsv1.Deployment{}
	err = r.Get(context.TODO(), params.NameMethod(r, vpa), existingDeployment)
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
		util.ReleaseVersionMatches(existingDeployment, r.Config.ReleaseVersion) {
		return false, err
	}

	existingDeployment.Spec.Template.Spec = *expectedSpec
	existingDeployment.Spec.Replicas = &expectedReplicas

	r.UpdateAnnotations(existingDeployment)
	r.UpdateAnnotations(&existingDeployment.Spec.Template)
	err = r.Update(context.TODO(), existingDeployment)
	return err == nil, err
}

// CreateWebhookService will create the webhook service for the given
// VerticalPodAutoscalerController custom resource instance.
func (r *VerticalPodAutoscalerControllerReconciler) CreateWebhookService(vpa *autoscalingv1.VerticalPodAutoscalerController) error {
	klog.Infof("Creating VerticalPodAutoscalerController service: %s", WebhookServiceName)
	service := r.WebhookService(vpa)

	// Set VerticalPodAutoscalerController instance as the owner and controller.
	if err := controllerutil.SetControllerReference(vpa, service, r.Scheme); err != nil {
		return err
	}

	return r.Create(context.TODO(), service)
}

// UpdateWebhookService will retrieve the service for the given VerticalPodAutoscalerController
// custom resource instance and update it to match the expected spec if needed.
func (r *VerticalPodAutoscalerControllerReconciler) UpdateWebhookService(vpa *autoscalingv1.VerticalPodAutoscalerController) (updated bool, err error) {

	nn := types.NamespacedName{
		Name:      WebhookServiceName,
		Namespace: r.Config.Namespace,
	}
	existingService := &corev1.Service{}
	err = r.Get(context.TODO(), nn, existingService)
	if err != nil {
		return false, err
	}

	merged := existingService.DeepCopy()
	expected := r.WebhookService(vpa)
	// Only comparing service spec.ports, spec.selector, and annotations (including release version)
	merged.Spec.Ports = expected.Spec.Ports
	merged.Spec.Selector = expected.Spec.Selector
	r.UpdateServiceAnnotations(merged)
	if equality.Semantic.DeepEqual(existingService, merged) {
		return false, nil
	}

	err = r.Update(context.TODO(), merged)
	return err == nil, err
}

// CreateCAConfigMap will create the CA ConfigMap for the given
// VerticalPodAutoscalerController custom resource instance.
func (r *VerticalPodAutoscalerControllerReconciler) CreateCAConfigMap(vpa *autoscalingv1.VerticalPodAutoscalerController) error {
	klog.Infof("Creating VerticalPodAutoscalerController configmap: %s", CACertConfigMapName)
	cm := r.CAConfigMap(vpa)

	// Set VerticalPodAutoscalerController instance as the owner and controller.
	if err := controllerutil.SetControllerReference(vpa, cm, r.Scheme); err != nil {
		return err
	}

	return r.Create(context.TODO(), cm)
}

// UpdateCAConfigMap will retrieve the CA ConfigMap for the given VerticalPodAutoscalerController
// custom resource instance and update it to match the expected spec if needed.
func (r *VerticalPodAutoscalerControllerReconciler) UpdateCAConfigMap(vpa *autoscalingv1.VerticalPodAutoscalerController) (updated bool, err error) {

	nn := types.NamespacedName{
		Name:      CACertConfigMapName,
		Namespace: r.Config.Namespace,
	}
	existingCM := &corev1.ConfigMap{}
	err = r.Get(context.TODO(), nn, existingCM)
	if err != nil {
		return false, err
	}

	merged := existingCM.DeepCopy()
	// Only comparing annotations (including release version)
	r.UpdateConfigMapAnnotations(merged)
	if equality.Semantic.DeepEqual(existingCM, merged) {
		return false, nil
	}
	err = r.Update(context.TODO(), merged)
	return err == nil, err
}

// RecommenderName returns the expected NamespacedName for the deployment
// belonging to the given VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) RecommenderName(vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName {
	return types.NamespacedName{
		Name:      fmt.Sprintf("vpa-recommender-%s", vpa.Name),
		Namespace: r.Config.Namespace,
	}
}

// UpdaterName returns the expected NamespacedName for the deployment
// belonging to the given VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) UpdaterName(vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName {
	return types.NamespacedName{
		Name:      fmt.Sprintf("vpa-updater-%s", vpa.Name),
		Namespace: r.Config.Namespace,
	}
}

// AdmissionPluginName returns the expected NamespacedName for the deployment
// belonging to the given VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) AdmissionPluginName(vpa *autoscalingv1.VerticalPodAutoscalerController) types.NamespacedName {
	return types.NamespacedName{
		Name:      fmt.Sprintf("vpa-admission-plugin-%s", vpa.Name),
		Namespace: r.Config.Namespace,
	}
}

// RecommenderEnabled returns true if the recommender should be enabled
func (r *VerticalPodAutoscalerControllerReconciler) RecommenderEnabled(vpa *autoscalingv1.VerticalPodAutoscalerController) bool {
	return true
}

// UpdaterEnabled returns true if the recommender should be enabled
func (r *VerticalPodAutoscalerControllerReconciler) UpdaterEnabled(vpa *autoscalingv1.VerticalPodAutoscalerController) bool {
	return vpa.Spec.RecommendationOnly == nil || !*vpa.Spec.RecommendationOnly
}

// AdmissionPluginEnabled returns true if the recommender should be enabled
func (r *VerticalPodAutoscalerControllerReconciler) AdmissionPluginEnabled(vpa *autoscalingv1.VerticalPodAutoscalerController) bool {
	return vpa.Spec.RecommendationOnly == nil || !*vpa.Spec.RecommendationOnly
}

// UpdateAnnotations updates the annotations on the given object to the values
// currently expected by the controller.
func (r *VerticalPodAutoscalerControllerReconciler) UpdateAnnotations(obj metav1.Object) {
	annotations := obj.GetAnnotations()

	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[util.ReleaseVersionAnnotation] = r.Config.ReleaseVersion

	obj.SetAnnotations(annotations)
}

// UpdateServiceAnnotations updates the annotations on the given object to the values
// currently expected by the controller.
func (r *VerticalPodAutoscalerControllerReconciler) UpdateServiceAnnotations(obj metav1.Object) {
	annotations := obj.GetAnnotations()

	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[util.ReleaseVersionAnnotation] = r.Config.ReleaseVersion
	annotations[webhookCertAnnotationName] = WebhookCertSecretName

	obj.SetAnnotations(annotations)
}

// UpdateConfigMapAnnotations updates the annotations on the given object to the values
// currently expected by the controller.
func (r *VerticalPodAutoscalerControllerReconciler) UpdateConfigMapAnnotations(obj metav1.Object) {
	annotations := obj.GetAnnotations()

	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[util.ReleaseVersionAnnotation] = r.Config.ReleaseVersion
	annotations[cACertAnnotationName] = "true"

	obj.SetAnnotations(annotations)
}

// AutoscalerDeployment returns the expected deployment belonging to the given
// VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) AutoscalerDeployment(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *appsv1.Deployment {

	namespacedName := params.NameMethod(r, vpa)
	labels := map[string]string{
		"vertical-pod-autoscaler": vpa.Name,
		"app":                     params.AppName,
	}

	annotations := map[string]string{
		util.ReleaseVersionAnnotation: r.Config.ReleaseVersion,
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
func (r *VerticalPodAutoscalerControllerReconciler) DefaultVPAController() *autoscalingv1.VerticalPodAutoscalerController {
	var smf = DefaultSafetyMarginFraction
	var podcpu = DefaultPodMinCPUMillicores
	var podminmem = DefaultPodMinMemoryMb
	var recommendationOnly = DefaultRecommendationOnly
	var minReplicas = DefaultMinReplicas

	vpa := &autoscalingv1.VerticalPodAutoscalerController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.Config.Name,
			Namespace: r.Config.Namespace,
		},
	}
	vpa.Name = r.Config.Name
	vpa.Spec.SafetyMarginFraction = &smf
	vpa.Spec.PodMinCPUMillicores = &podcpu
	vpa.Spec.PodMinMemoryMb = &podminmem
	vpa.Spec.RecommendationOnly = &recommendationOnly
	vpa.Spec.MinReplicas = &minReplicas
	return vpa
}

// VPAPodSpec returns the expected podSpec for the deployment belonging
// to the given VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) VPAPodSpec(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec {
	args := params.GetArgs(vpa, r.Config)

	if r.Config.ExtraArgs != "" {
		args = append(args, r.Config.ExtraArgs)
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
				Image:           r.Config.Image,
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
				Resources: params.ResourceRequirements,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
					RunAsNonRoot: ptr.To(true),
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

// RecommenderControllerPodSpec returns the expected podSpec for the Recommender Controller deployment belonging
// to the given VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) RecommenderControllerPodSpec(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec {
	spec := r.VPAPodSpec(vpa, params)

	// Allow the user to override the resources of the container
	if (!reflect.DeepEqual(vpa.Spec.DeploymentOverrides.Recommender.Container.Resources, corev1.ResourceRequirements{})) {
		spec.Containers[0].Resources = vpa.Spec.DeploymentOverrides.Recommender.Container.Resources
	}

	// Append user args to our container args
	if len(vpa.Spec.DeploymentOverrides.Recommender.Container.Args) > 0 {
		spec.Containers[0].Args = append(spec.Containers[0].Args, vpa.Spec.DeploymentOverrides.Recommender.Container.Args...)
	}

	// Replace node selector, if specified
	if len(vpa.Spec.DeploymentOverrides.Recommender.NodeSelector) > 0 {
		spec.NodeSelector = vpa.Spec.DeploymentOverrides.Recommender.NodeSelector
	}

	// Replace tolerations, if specified
	if len(vpa.Spec.DeploymentOverrides.Recommender.Tolerations) > 0 {
		spec.Tolerations = vpa.Spec.DeploymentOverrides.Recommender.Tolerations
	}

	return spec
}

// UpdaterControllerPodSpec returns the expected podSpec for the Updater Controller deployment belonging
// to the given VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) UpdaterControllerPodSpec(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec {
	spec := r.VPAPodSpec(vpa, params)

	// Allow the user to override the resources of the container
	if (!reflect.DeepEqual(vpa.Spec.DeploymentOverrides.Updater.Container.Resources, corev1.ResourceRequirements{})) {
		spec.Containers[0].Resources = vpa.Spec.DeploymentOverrides.Updater.Container.Resources
	}

	// Append user args to our container args, overrides are possible by
	if len(vpa.Spec.DeploymentOverrides.Updater.Container.Args) > 0 {
		spec.Containers[0].Args = append(spec.Containers[0].Args, vpa.Spec.DeploymentOverrides.Updater.Container.Args...)
	}

	// Replace node selector, if specified
	if len(vpa.Spec.DeploymentOverrides.Updater.NodeSelector) > 0 {
		spec.NodeSelector = vpa.Spec.DeploymentOverrides.Updater.NodeSelector
	}

	// Replace tolerations, if specified
	if len(vpa.Spec.DeploymentOverrides.Updater.Tolerations) > 0 {
		spec.Tolerations = vpa.Spec.DeploymentOverrides.Updater.Tolerations
	}

	return spec
}

// AdmissionControllerPodSpec returns the expected podSpec for the Admission Controller deployment belonging
// to the given VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) AdmissionControllerPodSpec(vpa *autoscalingv1.VerticalPodAutoscalerController, params ControllerParams) *corev1.PodSpec {
	spec := r.VPAPodSpec(vpa, params)

	// Allow the user to override the resources of the container
	if (!reflect.DeepEqual(vpa.Spec.DeploymentOverrides.Admission.Container.Resources, corev1.ResourceRequirements{})) {
		spec.Containers[0].Resources = vpa.Spec.DeploymentOverrides.Admission.Container.Resources
	}

	// Append user args to our container args
	if len(vpa.Spec.DeploymentOverrides.Admission.Container.Args) > 0 {
		spec.Containers[0].Args = append(spec.Containers[0].Args, vpa.Spec.DeploymentOverrides.Admission.Container.Args...)
	}

	// Replace node selector, if specified
	if len(vpa.Spec.DeploymentOverrides.Admission.NodeSelector) > 0 {
		spec.NodeSelector = vpa.Spec.DeploymentOverrides.Admission.NodeSelector
	}

	// Replace tolerations, if specified
	if len(vpa.Spec.DeploymentOverrides.Admission.Tolerations) > 0 {
		spec.Tolerations = vpa.Spec.DeploymentOverrides.Admission.Tolerations
	}

	spec.Containers[0].Ports = append(spec.Containers[0].Ports, corev1.ContainerPort{
		ContainerPort: int32(AdmissionWebhookPort),
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

// WebhookService returns the expected service belonging to the given
// VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) WebhookService(vpa *autoscalingv1.VerticalPodAutoscalerController) *corev1.Service {
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      WebhookServiceName,
			Namespace: r.Config.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       443,
					TargetPort: intstr.FromInt(int(AdmissionWebhookPort)),
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
func (r *VerticalPodAutoscalerControllerReconciler) CAConfigMap(vpa *autoscalingv1.VerticalPodAutoscalerController) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CACertConfigMapName,
			Namespace: r.Config.Namespace,
		},
	}

	r.UpdateConfigMapAnnotations(cm)
	return cm
}

// objectReference returns a reference to the given object, but will set the
// configured deployment namesapce if no namespace was previously set.  This is
// useful for referencing cluster scoped objects in events without the events
// being created in the default namespace.
func (r *VerticalPodAutoscalerControllerReconciler) objectReference(obj runtime.Object) *corev1.ObjectReference {
	ref, err := reference.GetReference(r.Scheme, obj)
	if err != nil {
		klog.Errorf("Error creating object reference: %v", err)
		return nil
	}

	if ref != nil && ref.Namespace == "" {
		ref.Namespace = r.Config.Namespace
	}

	return ref
}

func makePort(proto *corev1.Protocol,
	port intstr.IntOrString,
	//nolint:unparam
	endPort int32) networkingv1.NetworkPolicyPort {
	r := networkingv1.NetworkPolicyPort{
		Protocol: proto,
		Port:     nil,
	}
	if port != intstr.FromInt32(0) && port != intstr.FromString("") && port != intstr.FromString("0") {
		r.Port = &port
	}
	if endPort != 0 {
		r.EndPort = ptr.To(endPort)
	}
	return r
}

// NetworkPolicies returns the expected networkpolicies belonging to the given
// VerticalPodAutoscalerController.
func (r *VerticalPodAutoscalerControllerReconciler) NetworkPolicies(vpa *autoscalingv1.VerticalPodAutoscalerController) []networkingv1.NetworkPolicy {
	protocolTCP := corev1.ProtocolTCP
	protocolUDP := corev1.ProtocolUDP
	var policies []networkingv1.NetworkPolicy
	// Default deny all.  Additional policies will add all allowed traffic
	policies = append(policies, networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.String(),
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vpa-default-deny",
			Namespace: r.Config.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"vertical-pod-autoscaler": vpa.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
				networkingv1.PolicyTypeIngress,
			},
		},
	})
	// All VPA pods should be able to reach the cluster DNS
	policies = append(policies, networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.String(),
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vpa-allow-egress-to-dns",
			Namespace: r.Config.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"vertical-pod-autoscaler": vpa.Name,
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "openshift-dns",
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"dns.operator.openshift.io/daemonset-dns": "default",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						makePort(&protocolTCP, intstr.FromInt32(5353), 0),
						makePort(&protocolUDP, intstr.FromInt32(5353), 0),
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
		},
	})
	// All VPA pods should be able to reach the API server
	policies = append(policies, networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.String(),
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vpa-allow-egress-to-api-server",
			Namespace: r.Config.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"vertical-pod-autoscaler": vpa.Name,
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						makePort(&protocolTCP, intstr.FromInt32(6443), 0),
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
		},
	})
	// The Admission webhook needs to be reachable by the API server
	policies = append(policies, networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.String(),
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vpa-allow-ingress-to-admission-webhook",
			Namespace: r.Config.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"vertical-pod-autoscaler": vpa.Name,
					"app":                     AdmissionControllerAppName,
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						makePort(&protocolTCP, intstr.FromInt32(int32(AdmissionWebhookPort)), 0),
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	})
	// The operand pods have metrics endpoints which we don't expose, but if a cluster admin has exposed them, they'll need this to be able to keep using them
	policies = append(policies, networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.String(),
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vpa-allow-ingress-to-metrics",
			Namespace: r.Config.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"vertical-pod-autoscaler": vpa.Name,
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						makePort(&protocolTCP, intstr.FromInt32(int32(8942)), 0),
						makePort(&protocolTCP, intstr.FromInt32(int32(8943)), 0),
						makePort(&protocolTCP, intstr.FromInt32(int32(8944)), 0),
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	})
	return policies
}
