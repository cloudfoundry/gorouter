package handlers_test

import (
	"fmt"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/handlers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validator", func() {
	var (
		validator handlers.Validator
		routes    []db.Route
		maxTTL    int
	)

	BeforeEach(func() {
		validator = handlers.NewValidator()
		maxTTL = 50

		routes = []db.Route{
			{
				Route:           "http://127.0.0.1/a/valid/route",
				IP:              "127.0.0.1",
				Port:            8080,
				TTL:             maxTTL,
				LogGuid:         "log_guid",
				RouteServiceUrl: "https://my-rs.example.com",
			},
		}
	})

	Describe(".ValidateCreate", func() {
		It("does not return an error if all route inputs are valid", func() {
			err := validator.ValidateCreate(routes, maxTTL)
			Expect(err).To(BeNil())
		})

		Context("when any route has an invalid value", func() {
			BeforeEach(func() {
				routes = append(routes, routes[0])
			})

			It("returns an error if any ttl is greater than max ttl", func() {
				routes[1].TTL = maxTTL + 1

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal(fmt.Sprintf("Max ttl is %d", maxTTL)))
			})

			It("returns an error if any ttl is less than 1", func() {
				routes[1].TTL = 0

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal("Request requires a ttl greater than 0"))
			})

			It("returns an error if any request does not have a route", func() {
				routes[0].Route = ""

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal("Each route request requires a valid route"))
			})

			It("returns an error if any port is less than 1", func() {
				routes[0].Port = 0

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal("Each route request requires a port greater than 0"))
			})

			It("returns an error if the path contains invalid characters", func() {
				routes[0].Route = "/foo/b ar"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal("Url cannot contain invalid characters"))
			})

			It("returns an error if the path is not valid", func() {
				routes[0].Route = "/foo/bar%"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(ContainSubstring("invalid URL"))
			})

			It("returns an error if the path contains a question mark", func() {
				routes[0].Route = "/foo/bar?a"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(ContainSubstring("cannot contain any of [?, #]"))
			})

			It("returns an error if the path contains a hash mark", func() {
				routes[0].Route = "/foo/bar#a"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(ContainSubstring("cannot contain any of [?, #]"))
			})

			It("returns an error if the route service url is not https", func() {
				routes[0].RouteServiceUrl = "http://my-rs.com/ab"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteServiceUrlInvalidError))
				Expect(err.Error()).To(Equal("Route service url must use HTTPS."))
			})

			It("returns an error if the route service url contains invalid characters", func() {
				routes[0].RouteServiceUrl = "https://my-rs.com/a  b"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteServiceUrlInvalidError))
				Expect(err.Error()).To(Equal("Url cannot contain invalid characters"))
			})

			It("returns an error if the route service url host is not valid", func() {
				routes[0].RouteServiceUrl = "https://my-rs%.com"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteServiceUrlInvalidError))
				Expect(err.Error()).To(ContainSubstring("hexadecimal escape in host"))
			})

			It("returns an error if the route service url path is not valid", func() {
				routes[0].RouteServiceUrl = "https://my-rs.com/ad%"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteServiceUrlInvalidError))
				Expect(err.Error()).To(ContainSubstring("invalid URL"))
			})

			It("returns an error if the route service url contains a question mark", func() {
				routes[0].RouteServiceUrl = "https://foo/bar?a"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteServiceUrlInvalidError))
				Expect(err.Error()).To(ContainSubstring("cannot contain any of [?, #]"))
			})

			It("returns an error if the route service url contains a hash mark", func() {
				routes[0].RouteServiceUrl = "https://foo/bar#a"

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.RouteServiceUrlInvalidError))
				Expect(err.Error()).To(ContainSubstring("cannot contain any of [?, #]"))
			})

			It("returns an error if any request does not have an IP", func() {
				routes[1].IP = ""

				err := validator.ValidateCreate(routes, maxTTL)
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal("Each route request requires an IP"))
			})
		})
	})

	Describe(".ValidateDelete", func() {
		It("does not return an error if all route inputs are valid", func() {
			err := validator.ValidateDelete(routes)
			Expect(err).To(BeNil())
		})

		Context("when any route has an invalid value", func() {
			BeforeEach(func() {
				routes = append(routes, routes[0])
			})

			It("returns an error if any request does not have a route", func() {
				routes[0].Route = ""

				err := validator.ValidateDelete(routes)
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal("Each route request requires a valid route"))
			})

			It("returns an error if any port is less than 1", func() {
				routes[0].Port = 0

				err := validator.ValidateDelete(routes)
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal("Each route request requires a port greater than 0"))
			})

			It("returns an error if any request does not have an IP", func() {
				routes[1].IP = ""

				err := validator.ValidateDelete(routes)
				Expect(err.Type).To(Equal(routing_api.RouteInvalidError))
				Expect(err.Error()).To(Equal("Each route request requires an IP"))
			})
		})
	})

	Describe("ValidateTcpRouteMapping", func() {
		var (
			tcpMapping db.TcpRouteMapping
		)

		BeforeEach(func() {
			tcpMapping = db.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000)
		})

		Context("when valid tcp mapping is passed", func() {
			It("does not return error", func() {
				err := validator.ValidateTcpRouteMapping([]db.TcpRouteMapping{tcpMapping})
				Expect(err).To(BeNil())
			})
		})

		Context("when invalid tcp route mappings are passed", func() {

			It("blows up when a host port is zero", func() {
				tcpMapping.HostPort = 0
				err := validator.ValidateTcpRouteMapping([]db.TcpRouteMapping{tcpMapping})
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.TcpRouteMappingInvalidError))
				Expect(err.Error()).To(Equal("Each tcp mapping requires a positive host port"))
			})

			It("blows up when a external port is zero", func() {
				tcpMapping.TcpRoute.ExternalPort = 0
				err := validator.ValidateTcpRouteMapping([]db.TcpRouteMapping{tcpMapping})
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.TcpRouteMappingInvalidError))
				Expect(err.Error()).To(Equal("Each tcp mapping requires a positive external port"))
			})

			It("blows up when host ip empty", func() {
				tcpMapping.HostIP = ""
				err := validator.ValidateTcpRouteMapping([]db.TcpRouteMapping{tcpMapping})
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.TcpRouteMappingInvalidError))
				Expect(err.Error()).To(Equal("Each tcp mapping requires a non empty host ip"))
			})

			It("blows up when group guid is empty", func() {
				tcpMapping.TcpRoute.RouterGroupGuid = ""
				err := validator.ValidateTcpRouteMapping([]db.TcpRouteMapping{tcpMapping})
				Expect(err).ToNot(BeNil())
				Expect(err.Type).To(Equal(routing_api.TcpRouteMappingInvalidError))
				Expect(err.Error()).To(Equal("Each tcp mapping requires a valid router group guid"))
			})
		})
	})
})
