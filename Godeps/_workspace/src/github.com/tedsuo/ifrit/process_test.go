package ifrit_test

import (
	"os"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/test_helpers"
)

var _ = Describe("Process", func() {
	Context("when a process is envoked", func() {
		var pinger test_helpers.PingChan
		var pingProc ifrit.Process
		var errChan chan error

		BeforeEach(func() {
			pinger = make(test_helpers.PingChan)
			pingProc = ifrit.Envoke(pinger)
			errChan = make(chan error)
		})

		Describe("Wait()", func() {
			BeforeEach(func() {
				go func() {
					errChan <- <-pingProc.Wait()
				}()
				go func() {
					errChan <- <-pingProc.Wait()
				}()
			})

			Context("when the process exits", func() {
				BeforeEach(func() {
					go func() {
						<-pinger
					}()
				})

				It("returns the run result upon completion", func() {
					err1 := <-errChan
					err2 := <-errChan
					Ω(err1).Should(Equal(test_helpers.PingerExitedFromPing))
					Ω(err2).Should(Equal(test_helpers.PingerExitedFromPing))
				})
			})
		})

		Describe("Signal()", func() {
			BeforeEach(func() {
				pingProc.Signal(os.Kill)
			})

			It("sends the signal to the runner", func() {
				err := <-pingProc.Wait()
				Ω(err).Should(Equal(test_helpers.PingerExitedFromSignal))
			})
		})
	})

	Context("when a process exits without closing ready", func() {
		var proc ifrit.Process

		BeforeEach(func(done Done) {
			proc = ifrit.Envoke(test_helpers.NoReadyRunner)
			close(done)
		})

		It("waits normally", func() {
			Ω(<-proc.Wait()).Should(Equal(test_helpers.NoReadyExitedNormally))
		})
	})
})
