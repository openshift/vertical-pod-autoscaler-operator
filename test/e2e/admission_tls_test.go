package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	tlspkg "github.com/openshift/controller-runtime-common/pkg/tls"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/util"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vpaNamespace        = "openshift-vertical-pod-autoscaler"
	admissionDeployment = "vpa-admission-plugin-default"
	verifyPollInterval  = 10 * time.Second
	verifyPollTimeout   = 5 * time.Minute
)

var e2eScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(e2eScheme))
	utilruntime.Must(configv1.Install(e2eScheme))
}

// newClient returns a controller-runtime client configured from the ambient
// kubeconfig (KUBECONFIG env var or in-cluster config).
func newClient() (client.Client, error) {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}
	return client.New(restConfig, client.Options{Scheme: e2eScheme})
}

// verifyAdmissionPluginTLSArgs polls until the admission plugin deployment's
// --min-tls-version and --tls-ciphers args match the TLS profile currently
// configured in the cluster APIServer object, or until the timeout is reached.
func verifyAdmissionPluginTLSArgs(ctx context.Context, cl client.Client) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		// Fetch the expected TLS profile from the cluster APIServer.
		apiServer := &configv1.APIServer{}
		g.Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, apiServer)).
			To(Succeed(), "get APIServer")

		profile, err := tlspkg.GetTLSProfileSpec(apiServer.Spec.TLSSecurityProfile)
		g.Expect(err).NotTo(HaveOccurred(), "resolve TLS profile spec")

		// Fetch the admission plugin deployment and read its container args.
		dep := &appsv1.Deployment{}
		g.Expect(cl.Get(ctx, types.NamespacedName{Name: admissionDeployment, Namespace: vpaNamespace}, dep)).
			To(Succeed(), "get admission plugin deployment")
		g.Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty(), "admission plugin deployment has no containers")

		args := dep.Spec.Template.Spec.Containers[0].Args

		// Verify --min-tls-version.
		if profile.MinTLSVersion != "" {
			g.Expect(args).To(ContainElement(fmt.Sprintf("--min-tls-version=%s", util.TLSVersionToArg(profile.MinTLSVersion))))
		} else {
			g.Expect(args).NotTo(ContainElement(HavePrefix("--min-tls-version=")))
		}

		// Verify --tls-ciphers.
		if len(profile.Ciphers) > 0 {
			expectedCiphers := strings.Join(util.TLSCiphersToArgs(profile.Ciphers), ",")
			g.Expect(args).To(ContainElement(fmt.Sprintf("--tls-ciphers=%s", expectedCiphers)))
		} else {
			g.Expect(args).NotTo(ContainElement(HavePrefix("--tls-ciphers=")))
		}
	}, verifyPollTimeout, verifyPollInterval).Should(Succeed())
}

var _ = Describe("Admission plugin TLS configuration", func() {
	var (
		ctx            context.Context
		cl             client.Client
		initialProfile *configv1.TLSSecurityProfile
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		cl, err = newClient()
		Expect(err).NotTo(HaveOccurred(), "create client")

		// Save the initial TLS security profile so scenario 3 can restore it.
		apiServer := &configv1.APIServer{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, apiServer)).
			To(Succeed(), "get APIServer for initial profile")
		if apiServer.Spec.TLSSecurityProfile != nil {
			initialProfile = apiServer.Spec.TLSSecurityProfile.DeepCopy()
		} else {
			initialProfile = nil
		}
	})

	It("matches the initial cluster TLS configuration", func() {
		verifyAdmissionPluginTLSArgs(ctx, cl)
	})

	It("updates when the APIServer TLS profile is changed to a custom profile", func() {
		By("patching the APIServer with a custom TLS profile")
		apiServer := &configv1.APIServer{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, apiServer)).
			To(Succeed())

		patch := client.MergeFrom(apiServer.DeepCopy())
		apiServer.Spec.TLSSecurityProfile = &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
					Ciphers: []string{
						"ECDHE-ECDSA-CHACHA20-POLY1305",
						"ECDHE-ECDSA-AES128-GCM-SHA256",
					},
				},
			},
		}
		Expect(cl.Patch(ctx, apiServer, patch)).To(Succeed(), "patch APIServer with custom TLS profile")

		DeferCleanup(func() {
			By("reverting the APIServer TLS profile to the initial configuration")
			current := &configv1.APIServer{}
			Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, current)).
				To(Succeed())
			revertPatch := client.MergeFrom(current.DeepCopy())
			current.Spec.TLSSecurityProfile = initialProfile
			Expect(cl.Patch(ctx, current, revertPatch)).To(Succeed(), "revert APIServer TLS profile")
		})

		By("verifying the admission plugin deployment reflects the custom profile")
		verifyAdmissionPluginTLSArgs(ctx, cl)
	})

	It("restores the original args when the APIServer TLS profile is reverted", func() {
		By("patching the APIServer with a custom TLS profile")
		apiServer := &configv1.APIServer{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, apiServer)).
			To(Succeed())

		patch := client.MergeFrom(apiServer.DeepCopy())
		apiServer.Spec.TLSSecurityProfile = &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
					Ciphers: []string{
						"ECDHE-ECDSA-CHACHA20-POLY1305",
						"ECDHE-ECDSA-AES128-GCM-SHA256",
					},
				},
			},
		}
		Expect(cl.Patch(ctx, apiServer, patch)).To(Succeed(), "patch APIServer with custom TLS profile")

		By("reverting the APIServer TLS profile to the initial configuration")
		current := &configv1.APIServer{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, current)).
			To(Succeed())
		revertPatch := client.MergeFrom(current.DeepCopy())
		current.Spec.TLSSecurityProfile = initialProfile
		Expect(cl.Patch(ctx, current, revertPatch)).To(Succeed(), "revert APIServer TLS profile")

		By("verifying the admission plugin deployment reflects the reverted profile")
		verifyAdmissionPluginTLSArgs(ctx, cl)
	})
})
