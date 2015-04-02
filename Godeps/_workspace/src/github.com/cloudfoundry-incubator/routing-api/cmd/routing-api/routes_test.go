package main_test

import (
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Routes API", func() {
	BeforeEach(func() {
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
			client.UpsertRoutes(routesToInsert)
			routes, getErr = client.Routes()
		})

		It("responds without an error", func() {
			Expect(getErr).NotTo(HaveOccurred())
		})

		It("fetches all of the routes", func() {
			Expect(routes).To(HaveLen(2))
			Expect(routes).To(ConsistOf(route1, route2))
		})

		It("deletes a route", func() {
			err := client.DeleteRoutes([]db.Route{route1})

			Expect(err).NotTo(HaveOccurred())

			routes, err = client.Routes()
			Expect(err).NotTo(HaveOccurred())
			Expect(routes).NotTo(ContainElement(route1))
		})
	})
})
