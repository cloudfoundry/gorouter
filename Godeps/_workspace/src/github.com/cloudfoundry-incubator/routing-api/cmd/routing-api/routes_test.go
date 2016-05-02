package main_test

import (
	"fmt"

	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/test_helpers"
	"github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/testrunner"
	"github.com/cloudfoundry-incubator/routing-api/models"
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
		var routes []models.Route
		var getErr error
		var route1, route2 models.Route

		BeforeEach(func() {
			route1 = models.Route{
				Route:   "a.b.c",
				Port:    33,
				IP:      "1.1.1.1",
				TTL:     55,
				LogGuid: "potato",
			}

			route2 = models.Route{
				Route:   "d.e.f",
				Port:    35,
				IP:      "2.2.2.2",
				TTL:     66,
				LogGuid: "banana",
			}

			routesToInsert := []models.Route{route1, route2}
			upsertErr := client.UpsertRoutes(routesToInsert)
			Expect(upsertErr).NotTo(HaveOccurred())
			routes, getErr = client.Routes()
		})

		It("responds without an error", func() {
			Expect(getErr).NotTo(HaveOccurred())
		})

		It("fetches all of the routes", func() {
			routingAPIRoute := models.Route{
				Route:   fmt.Sprintf("api.%s/routing", routingAPISystemDomain),
				Port:    routingAPIPort,
				IP:      routingAPIIP,
				TTL:     120,
				LogGuid: "my_logs",
			}

			Expect(routes).To(HaveLen(3))
			Expect(Routes(routes).ContainsAll(route1, route2, routingAPIRoute)).To(BeTrue())
		})

		It("deletes a route", func() {
			err := client.DeleteRoutes([]models.Route{route1})

			Expect(err).NotTo(HaveOccurred())

			routes, err = client.Routes()
			Expect(err).NotTo(HaveOccurred())
			Expect(Routes(routes).Contains(route1)).To(BeFalse())
		})

		It("rejects bad routes", func() {
			route3 := models.Route{
				Route:   "/foo/b ar",
				Port:    35,
				IP:      "2.2.2.2",
				TTL:     66,
				LogGuid: "banana",
			}

			err := client.UpsertRoutes([]models.Route{route3})
			Expect(err).To(HaveOccurred())

			routes, err = client.Routes()

			Expect(err).ToNot(HaveOccurred())
			Expect(Routes(routes).Contains(route1)).To(BeTrue())
			Expect(Routes(routes).Contains(route2)).To(BeTrue())
			Expect(Routes(routes).Contains(route3)).To(BeFalse())
		})

		Context("when a route has a context path", func() {
			var routeWithPath models.Route

			BeforeEach(func() {
				routeWithPath = models.Route{
					Route:   "host.com/path",
					Port:    51480,
					IP:      "1.2.3.4",
					TTL:     60,
					LogGuid: "logguid",
				}
				err := client.UpsertRoutes([]models.Route{routeWithPath})
				Expect(err).ToNot(HaveOccurred())
			})

			It("is present in the routes list", func() {
				var err error
				routes, err = client.Routes()
				Expect(err).ToNot(HaveOccurred())
				Expect(Routes(routes).Contains(routeWithPath)).To(BeTrue())
			})
		})
	})
})
