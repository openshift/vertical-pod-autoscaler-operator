package verticalpodautoscaler

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoscalingv1 "github.com/openshift/vertical-pod-autoscaler-operator/api/v1"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/util"
)

var _ = Describe("MutatingWebhookConfiguration management via Server-Side Apply", func() {
	const (
		webhookNamespace = "openshift-vertical-pod-autoscaler"
		releaseVersion   = "v100"
		serviceCAManager = "service-ca-operator"
	)

	// A CA bundle must be syntactically valid PEM, since the API server validates it.
	fakeCABundle := []byte(`-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAJ3Z0Z0Z0Z0ZMA0GCSqGSIb3DQEBCwUAMBExDzANBgNVBAMMBnRl
c3RDQTAeFw0yNTAxMDEwMDAwMDBaFw0zNTAxMDEwMDAwMDBaMBExDzANBgNVBAMM
BnRlc3RDQTCBnzANBgkqhkiG9w0BAQEFAAOBjQAwgYkCgYEAyQZ0Z0Z0Z0Z0Z0Z0
-----END CERTIFICATE-----
`)

	var (
		webhookClient client.Client
		reconciler    *VerticalPodAutoscalerControllerReconciler
		vpa           *autoscalingv1.VerticalPodAutoscalerController
		vpaRef        *corev1.ObjectReference
		webhookNN     types.NamespacedName
	)

	// apply invokes the production SSA path under test.
	apply := func() error {
		return reconciler.ensureWebhookConfiguration(context.Background(), vpa, vpaRef)
	}

	getWebhook := func() *admissionregistrationv1.MutatingWebhookConfiguration {
		GinkgoHelper()
		got := &admissionregistrationv1.MutatingWebhookConfiguration{}
		Expect(webhookClient.Get(context.Background(), webhookNN, got)).To(Succeed())
		return got
	}

	BeforeEach(func() {
		var err error
		webhookClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		webhookNN = types.NamespacedName{Name: WebhookConfigName}

		reconciler = &VerticalPodAutoscalerControllerReconciler{
			Client:   webhookClient,
			Scheme:   scheme.Scheme,
			Log:      ctrl.Log.WithName("test").WithName("webhook"),
			Recorder: events.NewFakeRecorder(128),
			Config: &Config{
				Name:           "default",
				Namespace:      webhookNamespace,
				ReleaseVersion: releaseVersion,
			},
		}

		vpa = &autoscalingv1.VerticalPodAutoscalerController{
			ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: webhookNamespace},
		}
		vpaRef = &corev1.ObjectReference{Kind: "VerticalPodAutoscalerController", Name: "default"}
	})

	AfterEach(func() {
		existing := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigName},
		}
		err := webhookClient.Delete(context.Background(), existing)
		if err != nil && !errors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("creates the webhook through Server-Side Apply", func() {
		Expect(apply()).To(Succeed())

		got := getWebhook()

		By("setting the service-ca injection and release version annotations")
		Expect(got.Annotations).To(HaveKeyWithValue(cACertAnnotationName, "true"))
		Expect(got.Annotations).To(HaveKeyWithValue(util.ReleaseVersionAnnotation, releaseVersion))

		By("configuring a single webhook pointing at the VPA webhook service")
		Expect(got.Webhooks).To(HaveLen(1))
		hook := got.Webhooks[0]
		Expect(hook.Name).To(Equal("vpa.k8s.io"))
		Expect(hook.ClientConfig.Service).NotTo(BeNil())
		Expect(hook.ClientConfig.Service.Name).To(Equal(WebhookServiceName))
		Expect(hook.ClientConfig.Service.Namespace).To(Equal(webhookNamespace))
		Expect(hook.ClientConfig.Service.Path).To(HaveValue(Equal("/")))
		Expect(hook.ClientConfig.Service.Port).To(HaveValue(Equal(int32(443))))

		By("leaving the CA bundle empty for service-ca to inject")
		Expect(hook.ClientConfig.CABundle).To(BeEmpty())

		By("setting the expected policies")
		Expect(hook.FailurePolicy).To(HaveValue(Equal(admissionregistrationv1.Ignore)))
		Expect(hook.SideEffects).To(HaveValue(Equal(admissionregistrationv1.SideEffectClassNone)))
		Expect(hook.MatchPolicy).To(HaveValue(Equal(admissionregistrationv1.Equivalent)))
		Expect(hook.TimeoutSeconds).To(HaveValue(Equal(int32(10))))
		Expect(hook.ReinvocationPolicy).To(HaveValue(Equal(admissionregistrationv1.NeverReinvocationPolicy)))
		Expect(hook.AdmissionReviewVersions).To(Equal([]string{"v1"}))

		By("claiming the selectors explicitly rather than leaving them to API defaulting")
		Expect(hook.NamespaceSelector).To(HaveValue(Equal(metav1.LabelSelector{})))
		Expect(hook.ObjectSelector).To(HaveValue(Equal(metav1.LabelSelector{})))

		By("writing via Server-Side Apply under the operator's field manager")
		// The literal is asserted deliberately: the field manager name is part of
		// the contract with service-ca, so renaming the constant must fail here.
		var applied bool
		for _, mf := range got.ManagedFields {
			if mf.Manager == "vpa-operator" && mf.Operation == metav1.ManagedFieldsOperationApply {
				applied = true
			}
		}
		Expect(applied).To(BeTrue(), "expected a managedFields entry applied by vpa-operator")

		By("not setting an owner reference, since the webhook is cluster-scoped")
		Expect(got.OwnerReferences).To(BeEmpty())
	})

	It("registers the correct operations and resources", func() {
		Expect(apply()).To(Succeed())

		rules := getWebhook().Webhooks[0].Rules
		Expect(rules).To(HaveLen(2))

		By("mutating pods on CREATE only")
		Expect(rules[0].APIGroups).To(Equal([]string{""}))
		Expect(rules[0].APIVersions).To(Equal([]string{"v1"}))
		Expect(rules[0].Resources).To(Equal([]string{"pods"}))
		Expect(rules[0].Operations).To(Equal([]admissionregistrationv1.OperationType{
			admissionregistrationv1.Create,
		}))
		Expect(rules[0].Operations).NotTo(ContainElement(admissionregistrationv1.Update),
			"pods must not be intercepted on UPDATE")
		Expect(rules[0].Scope).To(HaveValue(Equal(admissionregistrationv1.AllScopes)))

		By("validating VPA objects on CREATE and UPDATE")
		Expect(rules[1].APIGroups).To(Equal([]string{"autoscaling.k8s.io"}))
		Expect(rules[1].APIVersions).To(Equal([]string{"*"}))
		Expect(rules[1].Resources).To(Equal([]string{"verticalpodautoscalers"}))
		Expect(rules[1].Operations).To(ConsistOf(
			admissionregistrationv1.Create,
			admissionregistrationv1.Update,
		))
		Expect(rules[1].Scope).To(HaveValue(Equal(admissionregistrationv1.AllScopes)))
	})

	It("adopts an existing registration without changing its UID or CA bundle", func() {
		By("simulating a webhook previously self-registered by the admission controller")
		legacy := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigName},
			Webhooks: []admissionregistrationv1.MutatingWebhook{
				{
					Name: "vpa.k8s.io",
					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						Service: &admissionregistrationv1.ServiceReference{
							Name:      WebhookServiceName,
							Namespace: webhookNamespace,
						},
						CABundle: fakeCABundle,
					},
					Rules: []admissionregistrationv1.RuleWithOperations{
						{
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
								admissionregistrationv1.Update,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{""},
								APIVersions: []string{"v1"},
								Resources:   []string{"pods"},
							},
						},
					},
					AdmissionReviewVersions: []string{"v1"},
					SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
				},
			},
		}
		Expect(webhookClient.Create(context.Background(), legacy,
			client.FieldOwner("vpa-admission-controller"))).To(Succeed())
		originalUID := legacy.UID
		Expect(originalUID).NotTo(BeEmpty())

		By("applying as the operator")
		Expect(apply()).To(Succeed())

		got := getWebhook()

		By("adopting the object in place rather than recreating it")
		Expect(got.UID).To(Equal(originalUID))

		By("preserving the existing CA bundle")
		Expect(got.Webhooks[0].ClientConfig.CABundle).To(Equal(fakeCABundle))

		By("taking over the operator-managed fields")
		Expect(got.Annotations).To(HaveKeyWithValue(cACertAnnotationName, "true"))
		Expect(got.Webhooks[0].Rules).To(HaveLen(2))
		Expect(got.Webhooks[0].Rules[0].Operations).To(Equal([]admissionregistrationv1.OperationType{
			admissionregistrationv1.Create,
		}))
	})

	It("preserves a service-ca injected CA bundle across subsequent reconciliations", func() {
		Expect(apply()).To(Succeed())

		By("simulating service-ca injecting the CA bundle under its own field manager")
		injected := getWebhook()
		injected.Webhooks[0].ClientConfig.CABundle = fakeCABundle
		Expect(webhookClient.Update(context.Background(), injected,
			client.FieldOwner(serviceCAManager))).To(Succeed())

		By("reconciling again with a changed spec, as happens on operator upgrade")
		reconciler.Config.ReleaseVersion = "v101"
		Expect(apply()).To(Succeed())

		got := getWebhook()
		Expect(got.Annotations).To(HaveKeyWithValue(util.ReleaseVersionAnnotation, "v101"),
			"the operator's own field should be updated")
		Expect(got.Webhooks[0].ClientConfig.CABundle).To(Equal(fakeCABundle),
			"service-ca's CA bundle must survive a changing apply")

		By("reconciling again with no changes")
		Expect(apply()).To(Succeed())
		Expect(getWebhook().Webhooks[0].ClientConfig.CABundle).To(Equal(fakeCABundle))
	})

	It("repairs an emptied webhook list", func() {
		Expect(apply()).To(Succeed())

		By("removing every webhook entry, as a manual edit might")
		emptied := getWebhook()
		emptied.Webhooks = []admissionregistrationv1.MutatingWebhook{}
		Expect(webhookClient.Update(context.Background(), emptied)).To(Succeed())
		Expect(getWebhook().Webhooks).To(BeEmpty())

		By("restoring it on the next reconcile")
		Expect(apply()).To(Succeed())
		Expect(getWebhook().Webhooks).To(HaveLen(1))
	})

	It("reverts tampering with the fields it owns", func() {
		Expect(apply()).To(Succeed())

		By("tampering with policy and selector fields")
		tampered := getWebhook()
		tampered.Webhooks[0].FailurePolicy = ptr.To(admissionregistrationv1.Fail)
		tampered.Webhooks[0].TimeoutSeconds = ptr.To(int32(29))
		tampered.Webhooks[0].NamespaceSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"unwanted": "selector"},
		}
		tampered.Webhooks[0].ObjectSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"unwanted": "selector"},
		}
		tampered.Webhooks[0].ReinvocationPolicy = ptr.To(admissionregistrationv1.IfNeededReinvocationPolicy)
		tampered.Webhooks[0].Rules[0].Operations = []admissionregistrationv1.OperationType{
			admissionregistrationv1.Create,
			admissionregistrationv1.Update,
		}
		Expect(webhookClient.Update(context.Background(), tampered)).To(Succeed())

		By("restoring every tampered field on the next reconcile")
		Expect(apply()).To(Succeed())

		hook := getWebhook().Webhooks[0]
		Expect(hook.FailurePolicy).To(HaveValue(Equal(admissionregistrationv1.Ignore)))
		Expect(hook.TimeoutSeconds).To(HaveValue(Equal(int32(10))))
		// These three only revert because the operator claims them explicitly.
		// Their values match the API defaults, so leaving them unset would look
		// identical on a fresh apply but would leave the edits below in place.
		Expect(hook.NamespaceSelector).To(HaveValue(Equal(metav1.LabelSelector{})))
		Expect(hook.ObjectSelector).To(HaveValue(Equal(metav1.LabelSelector{})))
		Expect(hook.ReinvocationPolicy).To(HaveValue(Equal(admissionregistrationv1.NeverReinvocationPolicy)))
		Expect(hook.Rules[0].Operations).To(Equal([]admissionregistrationv1.OperationType{
			admissionregistrationv1.Create,
		}))
	})

	It("recreates the webhook after it is deleted", func() {
		Expect(apply()).To(Succeed())

		Expect(webhookClient.Delete(context.Background(), getWebhook())).To(Succeed())
		err := webhookClient.Get(context.Background(), webhookNN,
			&admissionregistrationv1.MutatingWebhookConfiguration{})
		Expect(errors.IsNotFound(err)).To(BeTrue())

		Expect(apply()).To(Succeed())
		Expect(getWebhook().Webhooks).To(HaveLen(1))
	})

	It("settles without resource version churn on repeated applies", func() {
		Expect(apply()).To(Succeed())
		settled := getWebhook().ResourceVersion

		for i := 0; i < 3; i++ {
			Expect(apply()).To(Succeed())
			Expect(getWebhook().ResourceVersion).To(Equal(settled),
				"apply #%d rewrote the object; a field likely disagrees with an API default", i+1)
		}
	})
})
