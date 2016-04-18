package utils_test

import (
	"github.com/cloudfoundry/gorouter/proxy/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RequestValidator", func() {
	Context("Host", func() {
		It("validates host header characters", func() {
			valid := []string{"1.2.3.4", "8.8.8.8:9000", "foo-bar.com", "foo_bar.com", "foo.com", "foo.com:80", "[::1]", "2001:4860:4860::8888",
				"abcdefghijklmnopqrstuvwxyz.com", "ABCDEFGHIJKLMNOPQRSTUVWXYZ.COM", "0123456789&!~*=%()$;+.com"}

			for _, h := range valid {
				Expect(utils.ValidHost(h)).To(BeTrue(), "expecting "+h+" to be valid host")
			}

			invalid := []string{"foo.com/bar", "", " ", "{foo.com}", "\xF0\x9F\x98\x81", "<script></script>"}
			for _, h := range invalid {
				Expect(utils.ValidHost(h)).To(BeFalse(), "expecting "+h+" to be invalid host")
			}
		})
	})
})
