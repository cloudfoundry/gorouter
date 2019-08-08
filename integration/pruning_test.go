package integration

import (
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
)

var _ = Describe("Pruning stale routes", func() {
	var (
		testState        *testState
		expectPruneAfter time.Duration

		tags map[string]string = nil
	)

	BeforeEach(func() {
		testState = NewTestState()
		testState.cfg.DropletStaleThreshold = 100 * time.Millisecond
		testState.cfg.PruneStaleDropletsInterval = 10 * time.Millisecond
		expectPruneAfter =
			testState.cfg.DropletStaleThreshold + testState.cfg.PruneStaleDropletsInterval
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	Context("when PruneStaleTlsRoutes is true", func() {
		BeforeEach(func() {
			testState.cfg.PruneStaleTlsRoutes = true
			testState.StartGorouter()
		})

		Specify("TLS route is removed after the ttl expires", func() {
			tlsApp := test.NewGreetApp(
				[]route.Uri{"something." + test_util.LocalhostDNS},
				testState.cfg.Port,
				testState.mbusClient,
				tags,
			)
			tlsApp.TlsRegister(testState.trustedBackendServerCertSAN)
			tlsApp.TlsListen(testState.trustedBackendTLSConfig)

			tlsApp.VerifyAppStatus(200)
			time.Sleep(expectPruneAfter)
			tlsApp.VerifyAppStatus(404)
		})
	})

	Context("when PruneStaleTlsRoutes is false", func() {
		BeforeEach(func() {
			testState.cfg.PruneStaleTlsRoutes = false
			testState.StartGorouter()
		})

		Specify("TLS route remains even after the ttl expires, but plaintext route is removed", func() {
			tlsApp := test.NewGreetApp(
				[]route.Uri{"tls-app." + test_util.LocalhostDNS},
				testState.cfg.Port,
				testState.mbusClient,
				tags,
			)
			tlsApp.TlsRegister(testState.trustedBackendServerCertSAN)
			tlsApp.TlsListen(testState.trustedBackendTLSConfig)

			plainTextApp := test.NewGreetApp(
				[]route.Uri{"plain-app." + test_util.LocalhostDNS},
				testState.cfg.Port,
				testState.mbusClient,
				tags,
			)
			plainTextApp.Register()
			plainTextApp.Listen()

			tlsApp.VerifyAppStatus(200)
			plainTextApp.VerifyAppStatus(200)

			time.Sleep(expectPruneAfter)

			tlsApp.VerifyAppStatus(200)
			plainTextApp.VerifyAppStatus(404)
		})
	})
})
