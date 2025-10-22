package operator

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	fakeconfigclient "github.com/openshift/client-go/config/clientset/versioned/fake"
	cvorm "github.com/openshift/cluster-version-operator/lib/resourcemerge"
	autoscalingv1 "github.com/openshift/vertical-pod-autoscaler-operator/api/v1"
	"github.com/openshift/vertical-pod-autoscaler-operator/internal/util"
	"github.com/openshift/vertical-pod-autoscaler-operator/test/helpers"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	utilruntime.Must(autoscalingv1.AddToScheme(scheme.Scheme))
}

var ClusterOperatorGroupResource = schema.ParseGroupResource("clusteroperators.config.openshift.io")

var (
	// ConditionTransitionTime is the default LastTransitionTime for
	// ClusterOperatorStatusCondition fixture objects.
	ConditionTransitionTime = metav1.NewTime(time.Date(
		2009, time.November, 10, 23, 0, 0, 0, time.UTC,
	))

	// Available is the list of expected conditions for the operator
	// when reporting as available and updated.
	AvailableConditions = []configv1.ClusterOperatorStatusCondition{
		{
			Type:               configv1.OperatorAvailable,
			Status:             configv1.ConditionTrue,
			LastTransitionTime: ConditionTransitionTime,
		},
		{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionFalse,
			LastTransitionTime: ConditionTransitionTime,
		},
		{
			Type:               configv1.OperatorDegraded,
			Status:             configv1.ConditionFalse,
			LastTransitionTime: ConditionTransitionTime,
		},
	}

	// DegradedConditions is the list of expected conditions for the operator
	// when reporting as degraded.
	DegradedConditions = []configv1.ClusterOperatorStatusCondition{
		{
			Type:               configv1.OperatorAvailable,
			Status:             configv1.ConditionTrue,
			LastTransitionTime: ConditionTransitionTime,
		},
		{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionFalse,
			LastTransitionTime: ConditionTransitionTime,
		},
		{
			Type:               configv1.OperatorDegraded,
			Status:             configv1.ConditionTrue,
			LastTransitionTime: ConditionTransitionTime,
		},
	}

	// ProgressingConditions is the list of expected conditions for the operator
	// when reporting as progressing.
	ProgressingConditions = []configv1.ClusterOperatorStatusCondition{
		{
			Type:               configv1.OperatorAvailable,
			Status:             configv1.ConditionTrue,
			LastTransitionTime: ConditionTransitionTime,
		},
		{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionTrue,
			LastTransitionTime: ConditionTransitionTime,
		},
		{
			Type:               configv1.OperatorDegraded,
			Status:             configv1.ConditionFalse,
			LastTransitionTime: ConditionTransitionTime,
		},
	}
)

const (
	VerticalPodAutoscalerName      = "test"
	VerticalPodAutoscalerNamespace = "test-namespace"
	ReleaseVersion                 = "v100.0.1"
)

var TestStatusReporterConfig = StatusReporterConfig{
	VerticalPodAutoscalerName:      VerticalPodAutoscalerName,
	VerticalPodAutoscalerNamespace: VerticalPodAutoscalerNamespace,
	ReleaseVersion:                 ReleaseVersion,
	RelatedObjects:                 []configv1.ObjectReference{},
}

// verticalPodAutoscaler is the default VerticalPodAutoscalerController object used in test setup.
var verticalPodAutoscaler = &autoscalingv1.VerticalPodAutoscalerController{
	TypeMeta: metav1.TypeMeta{
		Kind:       "VerticalPodAutoscalerController",
		APIVersion: "autoscaling.openshift.io/v1",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      VerticalPodAutoscalerName,
		Namespace: VerticalPodAutoscalerNamespace,
	},
}

// Common Kubernetes fixture objects.
var (
	deployment = helpers.NewTestDeployment(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("vpa-recommender-%s", VerticalPodAutoscalerName),
			Namespace: VerticalPodAutoscalerNamespace,
			Annotations: map[string]string{
				util.ReleaseVersionAnnotation: ReleaseVersion,
			},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 1,
			UpdatedReplicas:   1,
			Replicas:          1,
		},
	})
)

var _ = Describe("Testing Status", func() {

	Describe("Check VPA Recommender", func() {
		testCases := []struct {
			label        string
			expectedBool bool
			expectedErr  error
			objects      []runtime.Object
		}{
			{
				label:        "no vertical-pod-autoscaler",
				expectedBool: true,
				expectedErr:  nil,
				objects:      []runtime.Object{},
			},
			{
				label:        "no deployment",
				expectedBool: false,
				expectedErr:  nil,
				objects: []runtime.Object{
					verticalPodAutoscaler,
				},
			},
			{
				label:        "deployment wrong version",
				expectedBool: false,
				expectedErr:  nil,
				objects: []runtime.Object{
					verticalPodAutoscaler,
					deployment.WithReleaseVersion("vBAD").Object(),
				},
			},
			{
				label:        "deployment not available",
				expectedBool: false,
				expectedErr:  nil,
				objects: []runtime.Object{
					verticalPodAutoscaler,
					deployment.WithAvailableReplicas(0).Object(),
				},
			},
			{
				label:        "available and updated",
				expectedBool: true,
				expectedErr:  nil,
				objects: []runtime.Object{
					verticalPodAutoscaler,
					deployment.Object(),
				},
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should have correct values when: %s", tc.label), func() {
				reporter := &StatusReporter{
					client:       fakeclient.NewFakeClient(tc.objects...),
					configClient: fakeconfigclient.NewSimpleClientset(),
					config:       &TestStatusReporterConfig,
				}

				ok, err := reporter.CheckVPARecommender()

				Expect(ok).To(Equal(tc.expectedBool), "got %t, want %t", ok, tc.expectedBool)
				Expect(err).To(Equal(tc.expectedErr), "got %v, want %v", err, tc.expectedErr)
			})
		}
	})

	Describe("Testing Status Changes", func() {

		testCases := []struct {
			label      string
			expected   []configv1.ClusterOperatorStatusCondition
			transition func(*StatusReporter) error
		}{
			{
				label:    "available",
				expected: AvailableConditions,
				transition: func(r *StatusReporter) error {
					return r.available("AvailableReason", "available message")
				},
			},
			{
				label:    "progressing",
				expected: ProgressingConditions,
				transition: func(r *StatusReporter) error {
					return r.progressing("ProgressingReason", "progressing message")
				},
			},
			{
				label:    "degraded",
				expected: DegradedConditions,
				transition: func(r *StatusReporter) error {
					return r.degraded("DegradedReason", "degraded message")
				},
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should have correct status when operator is reporting as: %s", tc.label), func() {
				reporter := &StatusReporter{
					client:       fakeclient.NewFakeClient(),
					configClient: fakeconfigclient.NewSimpleClientset(),
					config:       &TestStatusReporterConfig,
				}

				err := tc.transition(reporter)
				Expect(err).NotTo(HaveOccurred())

				co, err := reporter.GetClusterOperator()
				Expect(err).NotTo(HaveOccurred())

				for _, cond := range tc.expected {
					ok := cvorm.IsOperatorStatusConditionPresentAndEqual(
						co.Status.Conditions, cond.Type, cond.Status,
					)

					Expect(ok).To(BeTrue(), "wrong status for condition: %s", cond.Type)
				}
			})
		}
	})

	Describe("Test Report Status", func() {

		testCases := []struct {
			label         string
			versionChange bool
			expectedBool  bool
			expectedErr   error
			expectedConds []configv1.ClusterOperatorStatusCondition
			clientObjs    []runtime.Object
			configObjs    []runtime.Object
		}{
			{
				label:         "deployment wrong version",
				versionChange: true,
				expectedBool:  false,
				expectedErr:   nil,
				expectedConds: ProgressingConditions,
				clientObjs: []runtime.Object{
					verticalPodAutoscaler,
					deployment.WithReleaseVersion("vWRONG").Object(),
				},
			},
			{
				label:         "available and updated",
				versionChange: true,
				expectedBool:  true,
				expectedErr:   nil,
				expectedConds: AvailableConditions,
				clientObjs: []runtime.Object{
					verticalPodAutoscaler,
					deployment.WithReleaseVersion(ReleaseVersion).Object(),
				},
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should report the status correctly when: %s", tc.label), func() {
				reporter := &StatusReporter{
					client:       fakeclient.NewFakeClient(tc.clientObjs...),
					configClient: fakeconfigclient.NewSimpleClientset(tc.configObjs...),
					config:       &TestStatusReporterConfig,
				}

				ok, err := reporter.ReportStatus()
				Expect(ok).To(Equal(tc.expectedBool))
				Expect(err).To(Equal(tc.expectedErr))

				// Check that the ClusterOperator status is created.
				co, err := reporter.GetClusterOperator()
				Expect(err).NotTo(HaveOccurred())

				// Check that all conditions have the expected status.
				for _, cond := range tc.expectedConds {
					ok := cvorm.IsOperatorStatusConditionPresentAndEqual(
						co.Status.Conditions, cond.Type, cond.Status,
					)
					Expect(ok).To(BeTrue())
				}

				// Check the LastTransitionTime of the Progressing condition.
				for _, v := range co.Status.Versions {
					if v.Name != "operator" {
						continue
					}

					p := cvorm.FindOperatorStatusCondition(
						co.Status.Conditions, configv1.OperatorProgressing,
					)

					Expect(p).NotTo(BeNil())

					switch tc.versionChange {
					case true:
						// If the version changed, the last transition time should
						// be more recent than the original.
						Expect(p.LastTransitionTime.Time).To(BeTemporally(">", ConditionTransitionTime.Time))

					case false:
						// If the version did not change, the last transition time
						// should remain unchanged.
						Expect(p.LastTransitionTime.Time).To(Equal(ConditionTransitionTime.Time))

					default:
						Fail("Unhandled test case; should not occur")
					}
				}
			})
		}
	})
})
