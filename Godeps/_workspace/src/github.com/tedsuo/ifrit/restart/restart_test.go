package restart_test

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/fake_runner"
	"github.com/tedsuo/ifrit/restart"
)

var _ = Describe("Restart", func() {
	var testRunner *fake_runner.TestRunner
	var restarter restart.Restarter
	var process ifrit.Process

	BeforeEach(func() {
		testRunner = fake_runner.NewTestRunner()
		restarter = restart.Restarter{
			Runner: testRunner,
			Load: func(runner ifrit.Runner, err error) ifrit.Runner {
				return nil
			},
		}
	})

	JustBeforeEach(func() {
		process = ifrit.Background(restarter)
	})

	AfterEach(func() {
		process.Signal(os.Kill)
		testRunner.EnsureExit()
		Eventually(process.Wait()).Should(Receive())
	})

	Describe("Process Behavior", func() {

		It("waits for the internal runner to be ready", func() {
			Consistently(process.Ready()).ShouldNot(BeClosed())
			testRunner.TriggerReady()
			Eventually(process.Ready()).Should(BeClosed())
		})
	})

	Describe("Load", func() {

		Context("when load returns a runner", func() {
			var loadedRunner *fake_runner.TestRunner
			var loadedRunners chan *fake_runner.TestRunner

			BeforeEach(func() {
				loadedRunners = make(chan *fake_runner.TestRunner, 1)
				restarter.Load = func(runner ifrit.Runner, err error) ifrit.Runner {
					select {
					case runner := <-loadedRunners:
						return runner
					default:
						return nil
					}
				}
				loadedRunner = fake_runner.NewTestRunner()
				loadedRunners <- loadedRunner
			})

			AfterEach(func() {
				loadedRunner.EnsureExit()
			})

			It("executes the returned Runner", func() {
				testRunner.TriggerExit(nil)
				loadedRunner.TriggerExit(nil)
			})
		})

		Context("when load returns nil", func() {
			BeforeEach(func() {
				restarter.Load = func(runner ifrit.Runner, err error) ifrit.Runner {
					return nil
				}
			})

			It("exits after running the initial Runner", func() {
				testRunner.TriggerExit(nil)
				Eventually(process.Wait()).Should(Receive(BeNil()))
			})
		})

		Context("when the load callback is nil", func() {
			BeforeEach(func() {
				restarter.Load = nil
			})

			It("exits with NoLoadCallback error", func() {
				Eventually(process.Wait()).Should(Receive(Equal(restart.ErrNoLoadCallback)))
			})
		})
	})
})
