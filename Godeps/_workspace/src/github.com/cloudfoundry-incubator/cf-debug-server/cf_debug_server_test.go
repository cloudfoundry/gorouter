package cf_debug_server_test

import (
	. "github.com/cloudfoundry-incubator/cf-debug-server"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"fmt"
	"io/ioutil"
	"net"
	"net/http"
)

var _ = Describe("CF Debug Server", func() {
	It("serves debug information", func() {
		address := "127.0.0.1:10003"

		SetAddr(address)

		Run()

		debugResponse, err := http.Get(fmt.Sprintf("http://%s/debug/pprof/goroutine", address))

		Ω(err).ShouldNot(HaveOccurred())

		debugInfo, err := ioutil.ReadAll(debugResponse.Body)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(debugInfo).Should(ContainSubstring("goroutine profile: total"))
	})

	Context("when the address is already in use", func() {
		It("panics", func() {
			address := "127.0.0.1:10004"

			_, err := net.Listen("tcp", address)
			Ω(err).ShouldNot(HaveOccurred())

			SetAddr(address)

			Ω(Run).Should(Panic())
		})
	})
})
