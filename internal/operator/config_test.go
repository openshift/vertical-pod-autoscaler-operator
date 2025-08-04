package operator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("New Config", func() {

	It("should get new non-nil config", func() {
		config := NewConfig()
		Expect(config).NotTo(BeNil())
		Expect(config.VerticalPodAutoscalerNamespace).NotTo(Equal(DefaultVerticalPodAutoscalerNamespace), "missing default for VerticalPodAutoscalerNamespace")
	})
})
