package route_test

import (
	"github.com/cloudfoundry/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("URIs", func() {

	Context("RouteKey", func() {

		var key route.Uri

		It("creates a route key based on uri", func() {
			key = route.Uri("dora.app.com").RouteKey()
			Expect(key.String()).To(Equal("dora.app.com"))

			key = route.Uri("dora.app.com/").RouteKey()
			Expect(key.String()).To(Equal("dora.app.com"))

			key = route.Uri("dora.app.com/v1").RouteKey()
			Expect(key.String()).To(Equal("dora.app.com/v1"))

		})

		Context("has a context path", func() {

			It("creates route key with context path", func() {
				key = route.Uri("dora.app.com/v1").RouteKey()
				Expect(key.String()).To(Equal("dora.app.com/v1"))

				key = route.Uri("dora.app.com/v1/abc").RouteKey()
				Expect(key.String()).To(Equal("dora.app.com/v1/abc"))
			})

			Context("has query string in uri", func() {

				It("strips query string for route key", func() {
					key = route.Uri("dora.app.com/v1?foo=bar").RouteKey()
					Expect(key.String()).To(Equal("dora.app.com/v1"))

					key = route.Uri("dora.app.com/v1?foo=bar&baz=bing").RouteKey()
					Expect(key.String()).To(Equal("dora.app.com/v1"))

					key = route.Uri("dora.app.com/v1/abc?foo=bar&baz=bing").RouteKey()
					Expect(key.String()).To(Equal("dora.app.com/v1/abc"))
				})

			})
		})

		Context("has query string in uri", func() {

			It("strips query string for route key", func() {
				key = route.Uri("dora.app.com?foo=bar").RouteKey()
				Expect(key.String()).To(Equal("dora.app.com"))

			})

		})

		Context("has mixed case in uri", func() {

			It("converts the uri to lowercase", func() {
				key = route.Uri("DoRa.ApP.CoM").RouteKey()
				Expect(key.String()).To(Equal("dora.app.com"))

				key = route.Uri("DORA.APP.COM/").RouteKey()
				Expect(key.String()).To(Equal("dora.app.com"))
			})

		})

	})
})
