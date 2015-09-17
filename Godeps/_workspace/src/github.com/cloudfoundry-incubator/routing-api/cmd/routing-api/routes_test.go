package main_test

import (
	"fmt"

	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	"github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/testrunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Routes API", func() {
	var routingAPIProcess ifrit.Process

	BeforeEach(func() {
		routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
		routingAPIProcess = ginkgomon.Invoke(routingAPIRunner)
	})

	AfterEach(func() {
		ginkgomon.Kill(routingAPIProcess)
	})

	Describe("Routes", func() {
		var routes []db.Route
		var getErr error
		var route1, route2 db.Route

		BeforeEach(func() {
			route1 = db.Route{
				Route:   "a.b.c",
				Port:    33,
				IP:      "1.1.1.1",
				TTL:     55,
				LogGuid: "potato",
			}

			route2 = db.Route{
				Route:   "d.e.f",
				Port:    35,
				IP:      "2.2.2.2",
				TTL:     66,
				LogGuid: "banana",
			}

			routesToInsert := []db.Route{route1, route2}
			upsertErr := client.UpsertRoutes(routesToInsert)
			Expect(upsertErr).NotTo(HaveOccurred())
			routes, getErr = client.Routes()
		})

		It("responds without an error", func() {
			Expect(getErr).NotTo(HaveOccurred())
		})

		It("fetches all of the routes", func() {
			routingAPIRoute := db.Route{
				Route:   fmt.Sprintf("api.%s/routing", routingAPISystemDomain),
				Port:    routingAPIPort,
				IP:      routingAPIIP,
				TTL:     120,
				LogGuid: "my_logs",
			}

			Expect(routes).To(HaveLen(3))
			Expect(routes).To(ConsistOf(route1, route2, routingAPIRoute))
		})

		It("deletes a route", func() {
			err := client.DeleteRoutes([]db.Route{route1})

			Expect(err).NotTo(HaveOccurred())

			routes, err = client.Routes()
			Expect(err).NotTo(HaveOccurred())
			Expect(routes).NotTo(ContainElement(route1))
		})

		It("rejects bad routes", func() {
			route3 := db.Route{
				Route:   "/foo/b ar",
				Port:    35,
				IP:      "2.2.2.2",
				TTL:     66,
				LogGuid: "banana",
			}

			err := client.UpsertRoutes([]db.Route{route3})
			Expect(err).To(HaveOccurred())

			routes, err = client.Routes()
			Expect(err).ToNot(HaveOccurred())
			Expect(routes).NotTo(ContainElement(route3))
			Expect(routes).To(ContainElement(route1))
			Expect(routes).To(ContainElement(route2))
		})

		Context("when a route has a context path", func() {
			var routeWithPath db.Route

			BeforeEach(func() {
				routeWithPath = db.Route{
					Route:   "host.com/path",
					Port:    51480,
					IP:      "1.2.3.4",
					TTL:     60,
					LogGuid: "logguid",
				}
				err := client.UpsertRoutes([]db.Route{routeWithPath})
				Expect(err).ToNot(HaveOccurred())
			})

			It("is present in the routes list", func() {
				var err error
				routes, err = client.Routes()
				Expect(err).ToNot(HaveOccurred())
				Expect(routes).To(ContainElement(routeWithPath))
			})
		})
	})
})
