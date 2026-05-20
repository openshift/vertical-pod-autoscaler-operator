package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAdmissionTLS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admission TLS E2E Suite")
}
