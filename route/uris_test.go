package route_test

import (
	. "github.com/cloudfoundry/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("URIs", func() {

	Context("RouteKey", func() {
		var key Uri

		It("creates a route key based on uri", func() {
			key = Uri("dora.app.com").RouteKey()
			Expect(key.String()).To(Equal("dora.app.com"))

			key = Uri("dora.app.com/").RouteKey()
			Expect(key.String()).To(Equal("dora.app.com"))

			key = Uri("dora.app.com/v1").RouteKey()
			Expect(key.String()).To(Equal("dora.app.com/v1"))

		})

		Context("has query string in uri", func() {

			It("strips query string for route key", func() {
				key = Uri("dora.app.com/v1?foo=bar").RouteKey()
				Expect(key.String()).To(Equal("dora.app.com/v1"))

				key = Uri("dora.app.com/v1?foo=bar&baz=bing").RouteKey()
				Expect(key.String()).To(Equal("dora.app.com/v1"))

			})
		})
	})
})
