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
				Route:   "http://127.0.0.1/a/valid/route",
				IP:      "127.0.0.1",
				Port:    8080,
				TTL:     maxTTL,
				LogGuid: "log_guid",
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
})
