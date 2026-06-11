package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	tlspkg "github.com/openshift/controller-runtime-common/pkg/tls"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/util"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
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

var customTLSProfile = &configv1.TLSSecurityProfile{
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

// newClient returns a controller-runtime client configured from the ambient
// kubeconfig (KUBECONFIG env var or in-cluster config).
func newClient() (client.Client, error) {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}
	return client.New(restConfig, client.Options{Scheme: e2eScheme})
}

var featureGateGVR = schema.GroupVersionResource{
	Group:    "config.openshift.io",
	Version:  "v1",
	Resource: "featuregates",
}

// IsFeatureGateEnabled returns true if the named feature gate appears in the
// enabled list of any version in the featuregates.config.openshift.io/cluster
// status.
func IsFeatureGateEnabled(t *testing.T, config *rest.Config, name string) bool {
	t.Helper()
	dynClient, err := dynamic.NewForConfig(config)
	require.NoError(t, err)
	fg, err := dynClient.Resource(featureGateGVR).Get(context.TODO(), "cluster", metav1.GetOptions{})
	require.NoError(t, err)
	featureGates, found, err := unstructured.NestedSlice(fg.Object, "status", "featureGates")
	require.NoError(t, err)
	if !found {
		return false
	}
	for _, versionEntry := range featureGates {
		entry, ok := versionEntry.(map[string]interface{})
		if !ok {
			continue
		}
		enabled, found, err := unstructured.NestedSlice(entry, "enabled")
		if err != nil || !found {
			continue
		}
		for _, gate := range enabled {
			gateMap, ok := gate.(map[string]interface{})
			if !ok {
				continue
			}
			if gateName, _, _ := unstructured.NestedString(gateMap, "name"); gateName == name {
				return true
			}
		}
	}
	return false
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

// verifyAdmissionPluginNoTLSArgs polls until the admission plugin deployment
// has no --min-tls-version or --tls-ciphers args, which is expected when the
// operator is not honoring the cluster TLS profile.
func verifyAdmissionPluginNoTLSArgs(ctx context.Context, cl client.Client) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		dep := &appsv1.Deployment{}
		g.Expect(cl.Get(ctx, types.NamespacedName{Name: admissionDeployment, Namespace: vpaNamespace}, dep)).
			To(Succeed(), "get admission plugin deployment")
		g.Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty(), "admission plugin deployment has no containers")

		args := dep.Spec.Template.Spec.Containers[0].Args
		g.Expect(args).NotTo(ContainElement(HavePrefix("--min-tls-version=")))
		g.Expect(args).NotTo(ContainElement(HavePrefix("--tls-ciphers=")))
	}, verifyPollTimeout, verifyPollInterval).Should(Succeed())
}

// patchAPIServerTLSAdherence patches the APIServer's TLSAdherence field and
// returns the original value for later restoration.
func patchAPIServerTLSAdherence(
	ctx context.Context, cl client.Client, policy configv1.TLSAdherencePolicy,
) {
	GinkgoHelper()

	apiServer := &configv1.APIServer{}
	Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, apiServer)).
		To(Succeed(), "get APIServer for TLSAdherence patch")

	patch := client.MergeFrom(apiServer.DeepCopy())
	apiServer.Spec.TLSAdherence = policy
	Expect(cl.Patch(ctx, apiServer, patch)).To(Succeed(), "patch APIServer TLSAdherence")
}

// patchAPIServerTLSProfile patches the APIServer's TLSSecurityProfile field.
func patchAPIServerTLSProfile(ctx context.Context, cl client.Client, profile *configv1.TLSSecurityProfile) {
	GinkgoHelper()

	apiServer := &configv1.APIServer{}
	Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, apiServer)).
		To(Succeed(), "get APIServer for TLS profile patch")

	patch := client.MergeFrom(apiServer.DeepCopy())
	apiServer.Spec.TLSSecurityProfile = profile
	Expect(cl.Patch(ctx, apiServer, patch)).To(Succeed(), "patch APIServer TLS profile")
}

type tlsTestCase struct {
	requireFeatureGate bool
	adherencePolicy    configv1.TLSAdherencePolicy
	customProfile      *configv1.TLSSecurityProfile
	revertProfile      bool
	expectTLSArgs      bool
}

var _ = Describe("Admission plugin TLS configuration", func() {
	var (
		ctx              context.Context
		cl               client.Client
		initialProfile   *configv1.TLSSecurityProfile
		initialAdherence configv1.TLSAdherencePolicy
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		cl, err = newClient()
		Expect(err).NotTo(HaveOccurred(), "create client")

		// Save the initial TLS settings so tests can restore them.
		apiServer := &configv1.APIServer{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: tlspkg.APIServerName}, apiServer)).
			To(Succeed(), "get APIServer for initial state")
		if apiServer.Spec.TLSSecurityProfile != nil {
			initialProfile = apiServer.Spec.TLSSecurityProfile.DeepCopy()
		} else {
			initialProfile = nil
		}
		initialAdherence = apiServer.Spec.TLSAdherence
	})

	DescribeTable("respects TLSAdherence policy",
		func(tc tlsTestCase) {
			if tc.requireFeatureGate && !IsFeatureGateEnabled(suiteT, suiteConfig, "TLSAdherence") {
				Skip("TLSAdherence feature gate is not enabled")
			}

			patchAPIServerTLSAdherence(ctx, cl, tc.adherencePolicy)
			DeferCleanup(func() {
				By("reverting TLSAdherence to the initial value")
				patchAPIServerTLSAdherence(ctx, cl, initialAdherence)
			})

			if tc.customProfile != nil {
				By("patching the APIServer with a custom TLS profile")
				patchAPIServerTLSProfile(ctx, cl, tc.customProfile)
				DeferCleanup(func() {
					By("reverting the APIServer TLS profile to the initial configuration")
					patchAPIServerTLSProfile(ctx, cl, initialProfile)
				})
			}

			if tc.revertProfile {
				By("reverting the APIServer TLS profile to the initial configuration")
				patchAPIServerTLSProfile(ctx, cl, initialProfile)
			}

			if tc.expectTLSArgs {
				By("verifying the admission plugin deployment has the expected TLS args")
				verifyAdmissionPluginTLSArgs(ctx, cl)
			} else {
				By("verifying the admission plugin deployment has no TLS args")
				verifyAdmissionPluginNoTLSArgs(ctx, cl)
			}
		},
		Entry("StrictAllComponents: matches cluster TLS config", tlsTestCase{
			requireFeatureGate: true,
			adherencePolicy:    configv1.TLSAdherencePolicyStrictAllComponents,
			expectTLSArgs:      true,
		}),
		Entry("StrictAllComponents: updates with custom profile", tlsTestCase{
			requireFeatureGate: true,
			adherencePolicy:    configv1.TLSAdherencePolicyStrictAllComponents,
			customProfile:      customTLSProfile,
			expectTLSArgs:      true,
		}),
		Entry("StrictAllComponents: restores args after revert", tlsTestCase{
			requireFeatureGate: true,
			adherencePolicy:    configv1.TLSAdherencePolicyStrictAllComponents,
			customProfile:      customTLSProfile,
			revertProfile:      true,
			expectTLSArgs:      true,
		}),
		Entry("NoOpinion: does not set TLS args", tlsTestCase{
			adherencePolicy: configv1.TLSAdherencePolicyNoOpinion,
			expectTLSArgs:   false,
		}),
		Entry("NoOpinion: does not set TLS args with custom profile", tlsTestCase{
			adherencePolicy: configv1.TLSAdherencePolicyNoOpinion,
			customProfile:   customTLSProfile,
			expectTLSArgs:   false,
		}),
	)
})
