package main_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"

	"testing"
)

var (
	gorouterPath string
	oauthServer  *ghttp.Server
)

func TestGorouter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gorouter Suite")
}

var _ = BeforeSuite(func() {
	path, err := gexec.Build("code.cloudfoundry.org/gorouter", "-race")
	Expect(err).ToNot(HaveOccurred())
	gorouterPath = path
	SetDefaultEventuallyTimeout(15 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(10 * time.Millisecond)
	oauthServer = setupTlsServer()
	oauthServer.HTTPTestServer.StartTLS()
})

var _ = AfterSuite(func() {
	if oauthServer != nil {
		oauthServer.Close()
	}
	gexec.CleanupBuildArtifacts()
})
