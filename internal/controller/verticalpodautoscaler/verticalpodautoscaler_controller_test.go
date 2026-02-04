package verticalpodautoscaler

import (
	"context"
	"fmt"
	"strings"
	"testing"

	autoscalingv1 "github.com/openshift/vertical-pod-autoscaler-operator/api/v1"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/util"
	"github.com/openshift/vertical-pod-autoscaler-operator/test/helpers"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	TestNamespace      = "test-namespace"
	TestReleaseVersion = "v100"
)

var (
	RecommendationMarginFraction     = float64(0.5)
	PodRecommendationMinCPUMilicores = float64(0.1)
	PodRecommendationMinMemoryMB     = float64(25)
	RecommendationOnly               = false
)
var TestReconcilerConfig = &Config{
	Name:           "test",
	Namespace:      TestNamespace,
	ReleaseVersion: TestReleaseVersion,
	Image:          "test/test:v100",
	Verbosity:      10,
}

func init() {
	utilruntime.Must(autoscalingv1.AddToScheme(scheme.Scheme))
}

func NewVerticalPodAutoscaler() *autoscalingv1.VerticalPodAutoscalerController {
	// TODO: Maybe just deserialize this from a YAML file?
	return &autoscalingv1.VerticalPodAutoscalerController{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VerticalPodAutoscalerController",
			APIVersion: "autoscaling.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: TestNamespace,
		},
		Spec: autoscalingv1.VerticalPodAutoscalerControllerSpec{
			SafetyMarginFraction: &RecommendationMarginFraction,
			PodMinCPUMillicores:  &PodRecommendationMinCPUMilicores,
			PodMinMemoryMb:       &PodRecommendationMinMemoryMB,
			RecommendationOnly:   &RecommendationOnly,
		},
	}
}

func includesStringWithPrefix(list []string, prefix string) bool {
	for i := range list {
		if strings.HasPrefix(list[i], prefix) {
			return true
		}
	}

	return false
}

func includeString(list []string, item string) bool {
	for i := range list {
		if list[i] == item {
			return true
		}
	}

	return false
}

func TestAdmissionArgs(t *testing.T) {
	vpa := NewVerticalPodAutoscaler()

	args := AdmissionPluginArgs(vpa, &Config{Namespace: TestNamespace})

	expected := []string{
		fmt.Sprintf("--kube-api-qps=%.01f", 25.0),
		fmt.Sprintf("--kube-api-burst=%.01f", 50.0),
		"--tls-cert-file=/data/tls-certs/tls.crt",
		"--tls-private-key=/data/tls-certs/tls.key",
		"--client-ca-file=/data/tls-ca-certs/service-ca.crt",
		"--webhook-timeout-seconds=10",
	}

	for _, e := range expected {
		if !includeString(args, e) {
			t.Fatalf("missing arg: %s from %s", e, args)
		}
	}
}

func TestRecommenderArgs(t *testing.T) {
	vpa := NewVerticalPodAutoscaler()

	args := RecommenderArgs(vpa, &Config{Namespace: TestNamespace})

	expected := []string{
		fmt.Sprintf("--recommendation-margin-fraction=%.01f", RecommendationMarginFraction),
		fmt.Sprintf("--pod-recommendation-min-cpu-millicores=%.01f", PodRecommendationMinCPUMilicores),
		fmt.Sprintf("--pod-recommendation-min-memory-mb=%.0f", PodRecommendationMinMemoryMB),
	}

	for _, e := range expected {
		if !includeString(args, e) {
			t.Fatalf("missing arg: %s from %s", e, args)
		}
	}

	expectedMissing := []string{
		"--scale-down-delay-after-delete",
		"--scale-down-delay-after-failure",
	}

	for _, e := range expectedMissing {
		if includesStringWithPrefix(args, e) {
			t.Fatalf("found arg expected to be missing: %s", e)
		}
	}
}

func TestOverrideResources(t *testing.T) {
	vpa := NewVerticalPodAutoscaler()
	r := newFakeReconciler(vpa, &appsv1.Deployment{})

	resourceOverride := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("90m"),
			corev1.ResourceMemory: resource.MustParse("90Mi"),
		},
	}

	vpa.Spec.DeploymentOverrides.Admission.Container.Resources = resourceOverride
	vpa.Spec.DeploymentOverrides.Recommender.Container.Resources = resourceOverride
	vpa.Spec.DeploymentOverrides.Updater.Container.Resources = resourceOverride

	for _, params := range controllerParams {
		t.Run(fmt.Sprintf("override %s resources", params.AppName), func(t *testing.T) {
			podSpec := params.PodSpecMethod(r, vpa, params)
			switch params.AppName {
			case "vpa-admission-controller":
				assert.Equal(t, vpa.Spec.DeploymentOverrides.Admission.Container.Resources, podSpec.Containers[0].Resources)
			case "vpa-recommender":
				assert.Equal(t, vpa.Spec.DeploymentOverrides.Recommender.Container.Resources, podSpec.Containers[0].Resources)
			case "vpa-updater":
				assert.Equal(t, vpa.Spec.DeploymentOverrides.Updater.Container.Resources, podSpec.Containers[0].Resources)

			}
		})
	}

}

func TestOverrideArgs(t *testing.T) {
	vpa := NewVerticalPodAutoscaler()
	r := newFakeReconciler(vpa, &appsv1.Deployment{})

	argsOverride := []string{"--kube-api-qps=6.0", "--kube-api-burst=11.0"}

	vpa.Spec.DeploymentOverrides.Admission.Container.Args = argsOverride
	vpa.Spec.DeploymentOverrides.Recommender.Container.Args = argsOverride
	vpa.Spec.DeploymentOverrides.Updater.Container.Args = argsOverride

	for _, params := range controllerParams {
		t.Run(fmt.Sprintf("override %s args", params.AppName), func(t *testing.T) {
			podSpec := params.PodSpecMethod(r, vpa, params)
			switch params.AppName {
			case "vpa-admission-controller":
				for _, arg := range vpa.Spec.DeploymentOverrides.Admission.Container.Args {
					assert.Contains(t, podSpec.Containers[0].Args, arg)
				}
			case "vpa-recommender":
				for _, arg := range vpa.Spec.DeploymentOverrides.Recommender.Container.Args {
					assert.Contains(t, podSpec.Containers[0].Args, arg)
				}
			case "vpa-updater":
				for _, arg := range vpa.Spec.DeploymentOverrides.Updater.Container.Args {
					assert.Contains(t, podSpec.Containers[0].Args, arg)
				}
			}
		})
	}

}

func TestOverrideNodeSelector(t *testing.T) {
	vpa := NewVerticalPodAutoscaler()
	r := newFakeReconciler(vpa, &appsv1.Deployment{})

	selOverride := map[string]string{"node-role.kubernetes.io/infra": ""}

	vpa.Spec.DeploymentOverrides.Admission.NodeSelector = selOverride
	vpa.Spec.DeploymentOverrides.Recommender.NodeSelector = selOverride
	vpa.Spec.DeploymentOverrides.Updater.NodeSelector = selOverride

	for _, params := range controllerParams {
		t.Run(fmt.Sprintf("override %s node selector", params.AppName), func(t *testing.T) {
			podSpec := params.PodSpecMethod(r, vpa, params)
			assert.Equal(t, selOverride, podSpec.NodeSelector)
		})
	}
}

func TestOverrideTolerations(t *testing.T) {
	vpa := NewVerticalPodAutoscaler()
	r := newFakeReconciler(vpa, &appsv1.Deployment{})

	tolOverride := []corev1.Toleration{
		{
			Key:      "node-role.kubernetes.io/infra",
			Effect:   corev1.TaintEffectNoSchedule,
			Operator: corev1.TolerationOpExists,
		},
	}

	vpa.Spec.DeploymentOverrides.Admission.Tolerations = tolOverride
	vpa.Spec.DeploymentOverrides.Recommender.Tolerations = tolOverride
	vpa.Spec.DeploymentOverrides.Updater.Tolerations = tolOverride

	for _, params := range controllerParams {
		t.Run(fmt.Sprintf("override %s tolerations", params.AppName), func(t *testing.T) {
			podSpec := params.PodSpecMethod(r, vpa, params)
			assert.Equal(t, tolOverride, podSpec.Tolerations)
		})
	}
}

// This test ensures we can actually get an autoscaler with fakeclient/client.
// fakeclient.NewFakeClientWithScheme will os.Exit(1) with invalid scheme.
func TestCanGetca(t *testing.T) {
	_ = fakeclient.NewFakeClient(NewVerticalPodAutoscaler())
}

// newFakeReconciler returns a new reconcile.Reconciler with a fake client
func newFakeReconciler(initObjects ...runtime.Object) *VerticalPodAutoscalerControllerReconciler {
	fakeClient := fakeclient.NewFakeClient(initObjects...)
	return &VerticalPodAutoscalerControllerReconciler{
		Client:   fakeClient,
		Scheme:   scheme.Scheme,
		Recorder: events.NewFakeRecorder(128),
		Config:   TestReconcilerConfig,
	}
}

// The only time Reconcile() should fail is if there's a problem calling the
// api; that failure mode is not currently captured in this test.
func TestReconcile(t *testing.T) {
	vpa := NewVerticalPodAutoscaler()
	dep1 := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vertical-pod-autoscaler-test",
			Namespace: TestNamespace,
			Annotations: map[string]string{
				util.ReleaseVersionAnnotation: "test-1",
			},
			Generation: 1,
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			UpdatedReplicas:    1,
			Replicas:           1,
			AvailableReplicas:  1,
		},
	}
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: TestNamespace,
			Name:      "test",
		},
	}
	cfg1 := Config{
		ReleaseVersion: "test-1",
		Name:           "test",
		Namespace:      TestNamespace,
	}
	cfg2 := Config{
		ReleaseVersion: "test-1",
		Name:           "test2",
		Namespace:      TestNamespace,
	}
	tCases := []struct {
		expectedError error
		expectedRes   reconcile.Result
		c             *Config
		d             *appsv1.Deployment
	}{
		// Case 0: should pass, returns {}, nil.
		{
			expectedError: nil,
			expectedRes:   reconcile.Result{},
			c:             &cfg1,
			d:             &dep1,
		},
		// Case 1: no vpa found, should pass, returns {}, nil.
		{
			expectedError: nil,
			expectedRes:   reconcile.Result{},
			c:             &cfg2,
			d:             &dep1,
		},
		// Case 2: no dep found, should pass, returns {}, nil.
		{
			expectedError: nil,
			expectedRes:   reconcile.Result{},
			c:             &cfg1,
			d:             &appsv1.Deployment{},
		},
	}
	for i, tc := range tCases {
		r := newFakeReconciler(vpa, tc.d)
		r.SetConfig(tc.c)
		res, err := r.Reconcile(context.TODO(), req)
		assert.Equal(t, tc.expectedRes, res, "case %v: expected res incorrect", i)
		assert.Equal(t, tc.expectedError, err, "case %v: expected err incorrect", i)
	}
}

func TestObjectReference(t *testing.T) {
	testCases := []struct {
		label     string
		object    runtime.Object
		reference *corev1.ObjectReference
	}{
		{
			label: "no namespace",
			object: &autoscalingv1.VerticalPodAutoscalerController{
				TypeMeta: metav1.TypeMeta{
					Kind:       "VerticalPodAutoscalerController",
					APIVersion: "autoscaling.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-scoped",
				},
			},
			reference: &corev1.ObjectReference{
				Kind:       "VerticalPodAutoscalerController",
				APIVersion: "autoscaling.openshift.io/v1",
				Name:       "cluster-scoped",
				Namespace:  TestNamespace,
			},
		},
		{
			label: "existing namespace",
			object: &autoscalingv1.VerticalPodAutoscalerController{
				TypeMeta: metav1.TypeMeta{
					Kind:       "VerticalPodAutoscalerController",
					APIVersion: "autoscaling.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-scoped",
					Namespace: "should-not-change",
				},
			},
			reference: &corev1.ObjectReference{
				Kind:       "VerticalPodAutoscalerController",
				APIVersion: "autoscaling.openshift.io/v1",
				Name:       "cluster-scoped",
				Namespace:  "should-not-change",
			},
		},
	}

	r := newFakeReconciler()

	for _, tc := range testCases {
		t.Run(tc.label, func(t *testing.T) {
			ref := r.objectReference(tc.object)
			if ref == nil {
				t.Error("could not create object reference")
			}

			if !equality.Semantic.DeepEqual(tc.reference, ref) {
				t.Errorf("got %v, want %v", ref, tc.reference)
			}
		})
	}
}

func TestUpdateAnnotations(t *testing.T) {
	deployment := helpers.NewTestDeployment(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-namespace",
		},
	})

	testCases := []struct {
		label    string
		object   metav1.Object
		expected map[string]string
	}{
		{
			label:  "no prior annotations",
			object: deployment.Object(),
			expected: map[string]string{
				util.ReleaseVersionAnnotation: TestReleaseVersion,
			},
		},
		{
			label: "missing version annotation",
			object: deployment.WithAnnotations(map[string]string{
				"some.other/annotation": "value",
			}).Object(),
			expected: map[string]string{
				util.ReleaseVersionAnnotation: TestReleaseVersion,
				"some.other/annotation":       "value",
			},
		},
		{
			label: "old version annotation",
			object: deployment.WithAnnotations(map[string]string{
				util.ReleaseVersionAnnotation: "vOLD",
			}).Object(),
			expected: map[string]string{
				util.ReleaseVersionAnnotation: TestReleaseVersion,
			},
		},
	}

	r := newFakeReconciler()

	for _, tc := range testCases {
		t.Run(tc.label, func(t *testing.T) {
			r.UpdateAnnotations(tc.object)

			got := tc.object.GetAnnotations()
			if !equality.Semantic.DeepEqual(got, tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
