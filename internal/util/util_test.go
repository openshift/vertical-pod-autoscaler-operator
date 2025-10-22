package util

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Utility Functions", func() {

	Describe("FilterString", func() {
		testCases := []struct {
			label    string
			needle   string
			haystack []string
			output   []string
			count    int
		}{
			{
				label:    "single instance",
				needle:   "foo",
				haystack: []string{"foo", "bar", "baz"},
				output:   []string{"bar", "baz"},
				count:    1,
			},
			{
				label:    "multiple instances",
				needle:   "foo",
				haystack: []string{"foo", "bar", "foo"},
				output:   []string{"bar"},
				count:    2,
			},
			{
				label:    "zero instances",
				needle:   "buzz",
				haystack: []string{"foo", "bar", "foo"},
				output:   []string{"foo", "bar", "foo"},
				count:    0,
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should handle the '%s' case", tc.label), func() {
				got, count := FilterString(tc.haystack, tc.needle)
				Expect(got).To(Equal(tc.output))
				Expect(count).To(Equal(tc.count))
			})
		}
	})

	Describe("ReleaseVersionMatches", func() {
		releaseVersion := "v100"
		deployment := &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test-namespace",
			},
		}

		testCases := []struct {
			label        string
			expectedBool bool
			annotations  map[string]string
		}{
			{
				label:        "no annotation",
				expectedBool: false,
				annotations:  nil,
			},
			{
				label:        "wrong version",
				expectedBool: false,
				annotations: map[string]string{
					ReleaseVersionAnnotation: "BAD",
				},
			},
			{
				label:        "correct version",
				expectedBool: true,
				annotations: map[string]string{
					ReleaseVersionAnnotation: releaseVersion,
				},
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should return: %t, for: %s", tc.expectedBool, tc.label), func() {
				deployment.SetAnnotations(tc.annotations)
				ok := ReleaseVersionMatches(deployment, releaseVersion)
				Expect(ok).To(Equal(tc.expectedBool))
			})
		}
	})

	Describe("DeploymentUpdated", func() {
		deployment := &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test",
				Namespace:  "test-namespace",
				Generation: 100,
			},
		}

		testCases := []struct {
			label        string
			expectedBool bool
			status       appsv1.DeploymentStatus
		}{
			{
				label:        "old generation",
				expectedBool: false,
				status: appsv1.DeploymentStatus{
					AvailableReplicas:  10,
					Replicas:           10,
					UpdatedReplicas:    10,
					ObservedGeneration: 10,
				},
			},
			{
				label:        "replicas not updated",
				expectedBool: false,
				status: appsv1.DeploymentStatus{
					Replicas:           10,
					UpdatedReplicas:    5,
					ObservedGeneration: 100,
				},
			},
			{
				label:        "no available replicas",
				expectedBool: false,
				status: appsv1.DeploymentStatus{
					AvailableReplicas:  0,
					Replicas:           10,
					UpdatedReplicas:    10,
					ObservedGeneration: 100,
				},
			},
			{
				label:        "available and updated",
				expectedBool: true,
				status: appsv1.DeploymentStatus{
					AvailableReplicas:  10,
					Replicas:           10,
					UpdatedReplicas:    10,
					ObservedGeneration: 100,
				},
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should return: %t, for: %s", tc.expectedBool, tc.label), func() {
				deployment.Status = tc.status
				ok := DeploymentUpdated(deployment)
				Expect(ok).To(Equal(tc.expectedBool))
			})
		}
	})

	Describe("ResetProgressingTime", func() {
		ConditionTransitionTime := metav1.NewTime(time.Date(
			2009, time.November, 10, 23, 0, 0, 0, time.UTC,
		))

		testCases := []struct {
			label      string
			conditions []configv1.ClusterOperatorStatusCondition
		}{
			{
				label:      "no progressing condition",
				conditions: []configv1.ClusterOperatorStatusCondition{},
			},
			{
				label: "existing progressing condition",
				conditions: []configv1.ClusterOperatorStatusCondition{
					{
						Type:               configv1.OperatorProgressing,
						Status:             configv1.ConditionFalse,
						LastTransitionTime: ConditionTransitionTime,
					},
				},
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should ensure a Progressing condition exists with an updated timestamp: %s", tc.label), func() {
				ResetProgressingTime(&tc.conditions)

				found := false

				for _, c := range tc.conditions {
					if c.Type != configv1.OperatorProgressing {
						continue
					}

					found = true

					Expect(c.LastTransitionTime.Time).To(BeTemporally(">", ConditionTransitionTime.Time), "expected Progressing condition transition time update")
				}

				Expect(found).To(BeTrue(), "did not find expected Progressing condition")

			})
		}
	})

	Describe("ArgExists", func() {
		testCases := []struct {
			label    string
			args     []string
			prefix   string
			expected bool
		}{
			{
				label:    "argument exists",
				args:     []string{"--kube-api-qps=25.0", "--kube-api-burst=50.0"},
				prefix:   "--kube-api-qps",
				expected: true,
			},
			{
				label:    "argument does not exist",
				args:     []string{"--kube-api-qps=25.0", "--kube-api-burst=50.0"},
				prefix:   "--kube-api-rate",
				expected: false,
			},
			{
				label:    "empty arguments",
				args:     []string{},
				prefix:   "--kube-api-qps",
				expected: false,
			},
			{
				label:    "prefix matches partially",
				args:     []string{"--kube-api-qps=25.0", "--kube-api-burst=50.0"},
				prefix:   "--kube-api",
				expected: false,
			},
			{
				label:    "prefix matches partially the other way",
				args:     []string{"--kube-api-qps=25.0", "--kube-api-burst=50.0"},
				prefix:   "kube-api-qps",
				expected: false,
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should return: %t, for: %s", tc.expected, tc.label), func() {
				result := ArgExists(tc.args, tc.prefix)
				Expect(result).To(Equal(tc.expected))
			})
		}
	})
})
