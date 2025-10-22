package verticalpodautoscaler

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"

	autoscalingv1 "github.com/openshift/vertical-pod-autoscaler-operator/api/v1"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/util"
	"github.com/openshift/vertical-pod-autoscaler-operator/test/helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
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

// newFakeReconciler returns a new reconcile.Reconciler with a fake client
func newFakeReconciler(initObjects ...runtime.Object) *VerticalPodAutoscalerControllerReconciler {
	fakeClient := fakeclient.NewFakeClient(initObjects...)
	return &VerticalPodAutoscalerControllerReconciler{
		Client:   fakeClient,
		Scheme:   scheme.Scheme,
		Recorder: record.NewFakeRecorder(128),
		Config:   TestReconcilerConfig,
	}
}

var _ = Describe("VerticalPodAutoscalerController Unit Tests", func() {

	// This test ensures we can actually get an autoscaler with fakeclient/client.
	// fakeclient.NewFakeClientWithScheme will os.Exit(1) with invalid scheme.
	It("should get new fake client", func() {
		Expect(func() {
			_ = fakeclient.NewFakeClient(NewVerticalPodAutoscaler())
		}).ToNot(Panic())
	})

	It("should generate the correct admission args", func() {
		vpa := NewVerticalPodAutoscaler()
		args := AdmissionPluginArgs(vpa, &Config{Namespace: TestNamespace})

		Expect(args).To(ContainElement("--kube-api-qps=25.0"))
		Expect(args).To(ContainElement("--kube-api-burst=50.0"))
		Expect(args).To(ContainElement("--tls-cert-file=/data/tls-certs/tls.crt"))
		Expect(args).To(ContainElement("--tls-private-key=/data/tls-certs/tls.key"))
		Expect(args).To(ContainElement("--client-ca-file=/data/tls-ca-certs/service-ca.crt"))
		Expect(args).To(ContainElement("--webhook-timeout-seconds=10"))
	})

	It("should generate the correct recommender args", func() {
		vpa := NewVerticalPodAutoscaler()
		args := RecommenderArgs(vpa, &Config{Namespace: TestNamespace})

		Expect(args).To(ContainElement(fmt.Sprintf("--recommendation-margin-fraction=%.01f", RecommendationMarginFraction)))
		Expect(args).To(ContainElement(fmt.Sprintf("--pod-recommendation-min-cpu-millicores=%.01f", PodRecommendationMinCPUMilicores)))
		Expect(args).To(ContainElement(fmt.Sprintf("--pod-recommendation-min-memory-mb=%.0f", PodRecommendationMinMemoryMB)))

		Expect(args).NotTo(ContainElement("--scale-down-delay-after-delete"))
		Expect(args).NotTo(ContainElement("--scale-down-delay-after-failure"))
	})

	Describe("When handling overrides", func() {
		var (
			vpa *autoscalingv1.VerticalPodAutoscalerController
			r   *VerticalPodAutoscalerControllerReconciler
		)

		BeforeEach(func() {
			vpa = NewVerticalPodAutoscaler()
			r = newFakeReconciler(vpa, &appsv1.Deployment{})
		})

		It("should override resources", func() {
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
				podSpec := params.PodSpecMethod(r, vpa, params)
				switch params.AppName {
				case "vpa-admission-controller":
					Expect(podSpec.Containers[0].Resources).To(Equal(vpa.Spec.DeploymentOverrides.Admission.Container.Resources))
				case "vpa-recommender":
					Expect(podSpec.Containers[0].Resources).To(Equal(vpa.Spec.DeploymentOverrides.Recommender.Container.Resources))
				case "vpa-updater":
					Expect(podSpec.Containers[0].Resources).To(Equal(vpa.Spec.DeploymentOverrides.Updater.Container.Resources))

				}
			}
		})

		It("should override args", func() {
			argsOverride := []string{"--kube-api-qps=6.0", "--kube-api-burst=11.0"}

			vpa.Spec.DeploymentOverrides.Admission.Container.Args = argsOverride
			vpa.Spec.DeploymentOverrides.Recommender.Container.Args = argsOverride
			vpa.Spec.DeploymentOverrides.Updater.Container.Args = argsOverride

			for _, params := range controllerParams {
				podSpec := params.PodSpecMethod(r, vpa, params)
				switch params.AppName {
				case "vpa-admission-controller":
					for _, arg := range vpa.Spec.DeploymentOverrides.Admission.Container.Args {
						Expect(podSpec.Containers[0].Args).To(ContainElement(arg))
					}
				case "vpa-recommender":
					for _, arg := range vpa.Spec.DeploymentOverrides.Recommender.Container.Args {
						Expect(podSpec.Containers[0].Args).To(ContainElement(arg))
					}
				case "vpa-updater":
					for _, arg := range vpa.Spec.DeploymentOverrides.Updater.Container.Args {
						Expect(podSpec.Containers[0].Args).To(ContainElement(arg))
					}
				}
			}
		})

		It("should override node selectors", func() {
			selOverride := map[string]string{"node-role.kubernetes.io/infra": ""}

			vpa.Spec.DeploymentOverrides.Admission.NodeSelector = selOverride
			vpa.Spec.DeploymentOverrides.Recommender.NodeSelector = selOverride
			vpa.Spec.DeploymentOverrides.Updater.NodeSelector = selOverride

			for _, params := range controllerParams {
				podSpec := params.PodSpecMethod(r, vpa, params)
				Expect(selOverride).To(Equal(podSpec.NodeSelector))
			}
		})

		It("should override tolerations", func() {
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
				podSpec := params.PodSpecMethod(r, vpa, params)
				Expect(tolOverride).To(Equal(podSpec.Tolerations))
			}
		})
	})

	Describe("Reconciler Function", func() {

		// The only time Reconcile() should fail is if there's a problem calling the
		// api; that failure mode is not currently captured in this test.
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
			label       string
			expectedRes reconcile.Result
			c           *Config
			d           *appsv1.Deployment
		}{
			{
				label:       "vpa and deployment found, returns {}, nil",
				expectedRes: reconcile.Result{},
				c:           &cfg1,
				d:           &dep1,
			},
			{
				label:       "no vpa found, returns {}, nil",
				expectedRes: reconcile.Result{},
				c:           &cfg2,
				d:           &dep1,
			},
			{
				label:       "no deployment found, returns {}, nil",
				expectedRes: reconcile.Result{},
				c:           &cfg1,
				d:           &appsv1.Deployment{},
			},
		}

		for _, tc := range tCases {
			It(fmt.Sprintf("should deploy when: %s", tc.label), func() {
				vpa := NewVerticalPodAutoscaler()
				r := newFakeReconciler(vpa, tc.d)
				r.SetConfig(tc.c)
				req := reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: TestNamespace,
						Name:      "test",
					},
				}

				res, err := r.Reconcile(context.TODO(), req)
				Expect(res).To(Equal(tc.expectedRes))
				Expect(err).NotTo(HaveOccurred())
			})
		}

	})

	Describe("Object Reference", func() {

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
			It(fmt.Sprintf("should create object reference when: %s", tc.label), func() {
				ref := r.objectReference(tc.object)
				Expect(ref).NotTo(BeNil())
				Expect(ref).To(Equal(tc.reference))
			})
		}
	})

	Describe("Update Annotations", func() {

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

		expected := map[string]string{
			util.CriticalPodAnnotation:    "",
			util.ReleaseVersionAnnotation: TestReleaseVersion,
		}

		testCases := []struct {
			label  string
			object metav1.Object
		}{
			{
				label:  "no prior annotations exist",
				object: deployment.Object(),
			},
			{
				label: "missing version annotation",
				object: deployment.WithAnnotations(map[string]string{
					util.CriticalPodAnnotation: "",
				}).Object(),
			},
			{
				label: "missing critical-pod annotation",
				object: deployment.WithAnnotations(map[string]string{
					util.ReleaseVersionAnnotation: TestReleaseVersion,
				}).Object(),
			},
			{
				label: "version annotation is old",
				object: deployment.WithAnnotations(map[string]string{
					util.ReleaseVersionAnnotation: "vOLD",
				}).Object(),
			},
		}

		r := newFakeReconciler()

		for _, tc := range testCases {
			It(fmt.Sprintf("should update the annotations when: %s", tc.label), func() {
				r.UpdateAnnotations(tc.object)
				Expect(tc.object.GetAnnotations()).To(Equal(expected))
			})
		}
	})

})
