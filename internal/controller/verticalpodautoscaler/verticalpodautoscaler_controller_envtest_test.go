package verticalpodautoscaler

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	autoscalingv1 "github.com/openshift/vertical-pod-autoscaler-operator/api/v1"
)

var _ = Describe("VerticalPodAutoscalerController Controller", Ordered, func() {

	Describe("VPA Controller creation upon startup", func() {
		const (
			timeout                 = time.Second * 5
			interval                = time.Millisecond * 250
			expectedAnnotationValue = "true"
		)

		var (
			testCtx       context.Context
			testCancel    context.CancelFunc
			mgr           manager.Manager
			err           error
			vpaReconciler *VerticalPodAutoscalerControllerReconciler
		)

		vpaControllerNN := types.NamespacedName{
			Name:      "default",
			Namespace: "openshift-vertical-pod-autoscaler",
		}

		vpaNamespaceNN := types.NamespacedName{
			Name: "openshift-vertical-pod-autoscaler",
		}

		BeforeAll(func() {
			By("creating manager and reconciler to test controller")
			testCtx, testCancel = context.WithCancel(context.Background())

			// Create manager
			mgr, err = ctrl.NewManager(cfg, manager.Options{
				Scheme: testEnv.Scheme,
				Metrics: server.Options{
					BindAddress: "0",
				},
			})

			// Use manager's Client
			k8sClient = mgr.GetClient()
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient).NotTo(BeNil())

			// Create namespace
			vpaNs := &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "openshift-vertical-pod-autoscaler",
				},
			}
			Expect(k8sClient.Create(ctx, vpaNs)).To(Succeed())

			// Start manager
			go func() {
				defer GinkgoRecover()
				Expect(mgr.Start(testCtx)).To(Succeed())
			}()

			// Create reconciler
			vpaReconciler = &VerticalPodAutoscalerControllerReconciler{
				Client:   mgr.GetClient(),
				Log:      ctrl.Log.WithName("test").WithName("VerticalPodAutoscalerController"),
				Scheme:   mgr.GetScheme(),
				Recorder: mgr.GetEventRecorder("vpa-controller-test"),
				Config: &Config{
					Namespace: vpaControllerNN.Namespace,
					Name:      vpaControllerNN.Name,
					Image:     "test/test-image:latest",
				},
			}
		})

		AfterAll(func() {
			testCancel()
		})

		It("should create default controller and namespace annotation on first startup when annotation is not present", func() {
			// Trigger SetupWithManager()
			Expect(vpaReconciler.SetupWithManager(mgr)).To(Succeed())

			// Check that vpaController was created
			vpaController := &autoscalingv1.VerticalPodAutoscalerController{}
			Eventually(func() error {
				return k8sClient.Get(testCtx, vpaControllerNN, vpaController)
			}, timeout, interval).Should(Succeed())

			// Check for annotation
			vpaNamespace := &corev1.Namespace{}
			Eventually(func(g Gomega) {
				err := k8sClient.Get(testCtx, vpaNamespaceNN, vpaNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(vpaNamespace.Annotations).NotTo(BeNil())
				g.Expect(vpaNamespace.GetAnnotations()).To(HaveKeyWithValue(DefaultVPAControllerAnnotation, expectedAnnotationValue))
			}, timeout, interval).Should(Succeed())
		})

		It("should not create default controller when annotation is present", func() {
			vpaControllerToDelete := &autoscalingv1.VerticalPodAutoscalerController{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vpaControllerNN.Name,
					Namespace: vpaControllerNN.Namespace,
				},
			}

			// Add annotation to namespace
			vpaNamespace := &corev1.Namespace{}
			Expect(k8sClient.Get(testCtx, vpaNamespaceNN, vpaNamespace)).To(Succeed())
			if vpaNamespace.Annotations == nil {
				vpaNamespace.Annotations = make(map[string]string)
			}
			vpaNamespace.Annotations[DefaultVPAControllerAnnotation] = expectedAnnotationValue
			Expect(k8sClient.Update(testCtx, vpaNamespace)).To(Succeed())

			// Delete controller
			err := k8sClient.Delete(testCtx, vpaControllerToDelete)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			// Wait for deletion to propagate
			Eventually(func() bool {
				err = k8sClient.Get(testCtx, vpaControllerNN, vpaControllerToDelete)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			// Ensure annotation is in namespace
			Eventually(func(g Gomega) {
				vpaNamespace := &corev1.Namespace{}
				err = k8sClient.Get(testCtx, vpaNamespaceNN, vpaNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(vpaNamespace.GetAnnotations()).To(HaveKeyWithValue(DefaultVPAControllerAnnotation, expectedAnnotationValue))
			}, timeout, interval).Should(Succeed())

			// Trigger ensureVPAController
			Expect(vpaReconciler.ensureVPAController(testCtx)).To(Succeed())

			// Check that controller was not created
			vpaController := &autoscalingv1.VerticalPodAutoscalerController{}
			Consistently(func() bool {
				err = k8sClient.Get(testCtx, vpaControllerNN, vpaController)
				return errors.IsNotFound(err)
			}, "2s", "250ms").Should(BeTrue())
		})

		It("should add annotation to namespace and not create default controller when controller exists on startup", func() {
			// Create controller
			vpaController := &autoscalingv1.VerticalPodAutoscalerController{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vpaControllerNN.Name,
					Namespace: vpaControllerNN.Namespace,
				},
			}
			Expect(k8sClient.Create(testCtx, vpaController)).To(Succeed())

			// Wait for the cache to observe the newly created controller
			Eventually(func() error {
				return k8sClient.Get(testCtx, vpaControllerNN, vpaController)
			}, timeout, interval).Should(Succeed())
			originalResourceVersion := vpaController.ResourceVersion

			// Delete namespace annotation
			vpaNamespace := &corev1.Namespace{}
			Expect(k8sClient.Get(testCtx, vpaNamespaceNN, vpaNamespace)).To(Succeed())
			delete(vpaNamespace.Annotations, DefaultVPAControllerAnnotation)
			Expect(k8sClient.Update(testCtx, vpaNamespace)).To(Succeed())

			// Wait for the cache to observe the annotation removal
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(testCtx, vpaNamespaceNN, ns)).To(Succeed())
				g.Expect(ns.GetAnnotations()).NotTo(HaveKey(DefaultVPAControllerAnnotation))
			}, timeout, interval).Should(Succeed())

			// Trigger ensureVPAController
			Expect(vpaReconciler.ensureVPAController(testCtx)).To(Succeed())

			// Check that controller was not modified
			vpaController = &autoscalingv1.VerticalPodAutoscalerController{}
			Expect(k8sClient.Get(testCtx, vpaControllerNN, vpaController)).To(Succeed())
			Expect(vpaController.ResourceVersion).To(Equal(originalResourceVersion))

			// Check that annotation is added to namespace
			vpaNamespace = &corev1.Namespace{}
			Eventually(func(g Gomega) {
				err := k8sClient.Get(testCtx, vpaNamespaceNN, vpaNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(vpaNamespace.GetAnnotations()).To(HaveKeyWithValue(DefaultVPAControllerAnnotation, expectedAnnotationValue))
			}, timeout, interval).Should(Succeed())

		})
	})
})
