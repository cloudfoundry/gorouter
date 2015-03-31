package main_test

import (
	"github.com/cloudfoundry-incubator/routing-api"
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
			routesToInsert := []db.Route{route1}
			client.UpsertRoutes(routesToInsert)

			event, err := eventStream.Next()
			Expect(err).NotTo(HaveOccurred())

			Expect(event.Action).To(Equal("Upsert"))
			Expect(event.Route).To(Equal(route1))
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
			event1, err := eventStream.Next()
			Expect(err).ToNot(HaveOccurred())
			Expect(event1.Route).To(Equal(route1))

			client.UpsertRoutes([]db.Route{routeUpdated})
			event2, err := eventStream.Next()

			Expect(err).ToNot(HaveOccurred())
			Expect(event2.Action).To(Equal("Upsert"))
			Expect(event2.Route).To(Equal(routeUpdated))
		})

		It("gets events for deleted routes", func() {
			client.UpsertRoutes([]db.Route{route1})
			_, err := eventStream.Next()
			Expect(err).ToNot(HaveOccurred())

			client.DeleteRoutes([]db.Route{route1})
			event, err := eventStream.Next()

			Expect(err).ToNot(HaveOccurred())
			Expect(event.Action).To(Equal("Delete"))
			Expect(event.Route).To(Equal(route1))
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
			_, err := eventStream.Next()
			Expect(err).ToNot(HaveOccurred())

			event, err := eventStream.Next()

			Expect(err).ToNot(HaveOccurred())
			Expect(event.Action).To(Equal("Delete"))
			Expect(event.Route).To(Equal(routeExpire))
		})
	})
})
