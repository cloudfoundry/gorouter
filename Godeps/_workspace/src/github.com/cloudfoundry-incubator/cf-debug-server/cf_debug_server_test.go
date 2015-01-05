package cf_debug_server_test

import (
	"os"

	cf_debug_server "github.com/cloudfoundry-incubator/cf-debug-server"
	"github.com/tedsuo/ifrit"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
)

var _ = Describe("CF Debug Server", func() {
	var process ifrit.Process

	AfterEach(func() {
		if process != nil {
			process.Signal(os.Interrupt)
			<-process.Wait()
		}
	})

	Describe("AddFlags", func() {
		It("adds flags to the flagset", func() {
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			cf_debug_server.AddFlags(flags)

			f := flags.Lookup(cf_debug_server.DebugFlag)
			Ω(f).ShouldNot(BeNil())
		})
	})

	Describe("DebugAddress", func() {
		Context("when flags are not added", func() {
			It("returns the empty string", func() {
				flags := flag.NewFlagSet("test", flag.ContinueOnError)
				Ω(cf_debug_server.DebugAddress(flags)).Should(Equal(""))
			})
		})

		Context("when flags are added", func() {
			var flags *flag.FlagSet
			BeforeEach(func() {
				flags = flag.NewFlagSet("test", flag.ContinueOnError)
				cf_debug_server.AddFlags(flags)
			})

			Context("when set", func() {
				It("returns the address", func() {
					address := "127.0.0.1:10003"
					flags.Parse([]string{"-debugAddr", address})

					Ω(cf_debug_server.DebugAddress(flags)).Should(Equal(address))
				})
			})

			Context("when not set", func() {
				It("returns the empty string", func() {
					Ω(cf_debug_server.DebugAddress(flags)).Should(Equal(""))
				})
			})
		})
	})

	Describe("Run", func() {
		It("serves debug information", func() {
			address := "127.0.0.1:10003"

			err := cf_debug_server.Run(address)
			Ω(err).ShouldNot(HaveOccurred())

			debugResponse, err := http.Get(fmt.Sprintf("http://%s/debug/pprof/goroutine", address))
			Ω(err).ShouldNot(HaveOccurred())

			debugInfo, err := ioutil.ReadAll(debugResponse.Body)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(debugInfo).Should(ContainSubstring("goroutine profile: total"))
		})

		Context("when the address is already in use", func() {
			It("returns an error", func() {
				address := "127.0.0.1:10004"

				_, err := net.Listen("tcp", address)
				Ω(err).ShouldNot(HaveOccurred())

				err = cf_debug_server.Run(address)
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(BeAssignableToTypeOf(&net.OpError{}))
				netErr := err.(*net.OpError)
				Ω(netErr.Op).Should(Equal("listen"))
			})
		})
	})
})
