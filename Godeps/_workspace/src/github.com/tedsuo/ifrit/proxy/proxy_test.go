package proxy_test

import (
	"os"

	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/fake_runner"
	"github.com/tedsuo/ifrit/proxy"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Proxy", func() {
	var testRunner *fake_runner.TestRunner
	var process ifrit.Process
	var proxySignals chan os.Signal
	var receivedSignals <-chan os.Signal

	BeforeEach(func() {
		proxySignals = make(chan os.Signal, 1)
		testRunner = fake_runner.NewTestRunner()
		process = ifrit.Background(proxy.New(proxySignals, testRunner))
		receivedSignals = testRunner.WaitForCall()
		testRunner.TriggerReady()
	})

	It("sends the proxied signals to the embedded runner", func() {
		proxySignals <- os.Interrupt
		Eventually(receivedSignals).Should(Receive(Equal(os.Interrupt)))
	})

	It("sends the process signals to the embedded runner", func() {
		process.Signal(os.Interrupt)
		Eventually(receivedSignals).Should(Receive(Equal(os.Interrupt)))
	})
})
