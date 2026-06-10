package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	suiteT      *testing.T
	suiteConfig *rest.Config
)

func TestAdmissionTLS(t *testing.T) {
	suiteT = t
	var err error
	suiteConfig, err = ctrl.GetConfig()
	if err != nil {
		t.Fatalf("failed to get rest config: %v", err)
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admission TLS E2E Suite")
}
