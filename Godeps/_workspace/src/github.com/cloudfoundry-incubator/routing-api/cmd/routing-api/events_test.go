package main_test

import (
	"github.com/cloudfoundry-incubator/routing-api"
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

	var (
		eventStream routing_api.EventSource
		err         error
		route1      db.Route
	)

	Describe("SubscribeToEvents", func() {
		BeforeEach(func() {
			eventStream, err = client.SubscribeToEvents()
			Expect(err).NotTo(HaveOccurred())

			route1 = db.Route{
				Route:   "a.b.c",
				Port:    33,
				IP:      "1.1.1.1",
				TTL:     55,
				LogGuid: "potato",
			}
		})

		AfterEach(func() {
			eventStream.Close()
		})

		It("returns an eventstream", func() {
			expectedEvent := routing_api.Event{
				Action: "Upsert",
				Route:  route1,
			}
			routesToInsert := []db.Route{route1}
			client.UpsertRoutes(routesToInsert)

			Eventually(func() routing_api.Event {
				event, err := eventStream.Next()
				Expect(err).NotTo(HaveOccurred())
				return event
			}).Should(Equal(expectedEvent))
		})

		It("gets events for updated routes", func() {
			routeUpdated := db.Route{
				Route:   "a.b.c",
				Port:    33,
				IP:      "1.1.1.1",
				TTL:     85,
				LogGuid: "potato",
			}

			client.UpsertRoutes([]db.Route{route1})
			Eventually(func() db.Route {
				event, err := eventStream.Next()
				Expect(err).NotTo(HaveOccurred())
				return event.Route
			}).Should(Equal(route1))

			client.UpsertRoutes([]db.Route{routeUpdated})
			Eventually(func() db.Route {
				event, err := eventStream.Next()
				Expect(err).NotTo(HaveOccurred())
				return event.Route
			}).Should(Equal(routeUpdated))
		})

		It("gets events for deleted routes", func() {
			client.UpsertRoutes([]db.Route{route1})

			expectedEvent := routing_api.Event{
				Action: "Delete",
				Route:  route1,
			}
			client.DeleteRoutes([]db.Route{route1})
			Eventually(func() routing_api.Event {
				event, err := eventStream.Next()
				Expect(err).NotTo(HaveOccurred())
				return event
			}).Should(Equal(expectedEvent))
		})

		It("gets events for expired routes", func() {
			routeExpire := db.Route{
				Route:   "z.a.k",
				Port:    63,
				IP:      "42.42.42.42",
				TTL:     1,
				LogGuid: "Tomato",
			}

			client.UpsertRoutes([]db.Route{routeExpire})

			expectedEvent := routing_api.Event{
				Action: "Delete",
				Route:  routeExpire,
			}

			Eventually(func() routing_api.Event {
				event, err := eventStream.Next()
				Expect(err).NotTo(HaveOccurred())
				return event
			}).Should(Equal(expectedEvent))
		})
	})
})
