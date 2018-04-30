package integration

import (
	"path/filepath"
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
	testAssets   = filepath.Join("../test", "assets")
)

var _ = SynchronizedBeforeSuite(func() []byte {
	path, err := gexec.Build("code.cloudfoundry.org/gorouter", "-race")
	Expect(err).ToNot(HaveOccurred())
	return []byte(path)
}, func(data []byte) {
	gorouterPath = string(data)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(10 * time.Millisecond)
	oauthServer = setupTlsServer()
	oauthServer.HTTPTestServer.StartTLS()
})

var _ = SynchronizedAfterSuite(func() {
	if oauthServer != nil {
		oauthServer.Close()
	}
}, func() {
	gexec.CleanupBuildArtifacts()
})

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}
