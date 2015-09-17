package db_test

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DB", func() {
	Describe("etcd error", func() {
		var (
			etcd db.DB
			err  error
		)

		BeforeEach(func() {
			etcd, err = db.NewETCD(etcdRunner.NodeURLS(), 0)
			Expect(err).To(HaveOccurred())
		})

		It("should not return an etcd instance", func() {
			Expect(etcd).To(BeNil())
		})
	})

	Describe("etcd", func() {

		var (
			etcd db.DB
			err  error
		)

		BeforeEach(func() {
			etcd, err = db.NewETCD(etcdRunner.NodeURLS(), 10)
			Expect(err).NotTo(HaveOccurred())
			etcd.Connect()
		})

		AfterEach(func() {
			etcd.Disconnect()
		})

		Describe("Http Routes", func() {

			var (
				route db.Route
			)

			BeforeEach(func() {
				route = db.Route{
					Route:   "post_here",
					Port:    7000,
					IP:      "1.2.3.4",
					TTL:     50,
					LogGuid: "my-guid",
				}
			})

			Describe(".ReadRoutes", func() {
				It("Returns a empty list of routes", func() {
					routes, err := etcd.ReadRoutes()
					Expect(err).NotTo(HaveOccurred())
					Expect(routes).To(Equal([]db.Route{}))
				})

				Context("when only one entry is present", func() {
					BeforeEach(func() {
						route.Route = "next-route"
						route.IP = "9.8.7.6"
						route.Port = 12345

						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())
					})

					It("Returns a list with one route", func() {
						routes, err := etcd.ReadRoutes()
						Expect(err).NotTo(HaveOccurred())

						Expect(routes).To(ContainElement(route))
					})
				})

				Context("when the route contains a path", func() {
					BeforeEach(func() {
						route.Route = "route/path"
						route.IP = "9.8.7.6"
						route.Port = 12345

						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())
					})

					It("returns the route", func() {
						routes, err := etcd.ReadRoutes()
						Expect(err).NotTo(HaveOccurred())

						Expect(routes).To(ContainElement(route))
					})
				})

				Context("when multiple entries present", func() {
					var (
						route2 db.Route
						route3 db.Route
					)

					BeforeEach(func() {
						route.Route = "next-route"
						route.IP = "9.8.7.6"
						route.Port = 12345

						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())

						route2 = db.Route{
							Route:           "some-route",
							Port:            5500,
							IP:              "3.1.5.7",
							TTL:             1000,
							LogGuid:         "your-guid",
							RouteServiceUrl: "https://my-rs.com",
						}
						err = etcd.SaveRoute(route2)
						Expect(err).NotTo(HaveOccurred())

						route3 = db.Route{
							Route:   "some-other-route",
							Port:    5500,
							IP:      "3.1.5.7",
							TTL:     1000,
							LogGuid: "your-guid",
						}
						err = etcd.SaveRoute(route3)
						Expect(err).NotTo(HaveOccurred())
					})

					It("Returns a list with multiple routes", func() {
						routes, err := etcd.ReadRoutes()
						Expect(err).NotTo(HaveOccurred())

						Expect(routes).To(ContainElement(route))
						Expect(routes).To(ContainElement(route2))
						Expect(routes).To(ContainElement(route3))
					})
				})
			})

			Describe(".SaveRoute", func() {
				It("Creates a route if none exist", func() {
					err := etcd.SaveRoute(route)
					Expect(err).NotTo(HaveOccurred())

					node, err := etcdClient.Get(`/routes/post_here,1.2.3.4:7000`)
					Expect(err).NotTo(HaveOccurred())
					Expect(node.TTL).To(Equal(uint64(50)))
					Expect(node.Value).To(MatchJSON(`{
							"ip": "1.2.3.4",
							"route": "post_here",
							"port": 7000,
							"ttl": 50,
							"log_guid": "my-guid"
						}`))
				})

				Context("when a route has a route_service_url", func() {
					BeforeEach(func() {
						route = db.Route{
							Route:           "post_here",
							Port:            7000,
							IP:              "1.2.3.4",
							TTL:             50,
							LogGuid:         "my-guid",
							RouteServiceUrl: "https://my-rs.com",
						}
					})

					It("Creates a route if none exist", func() {
						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())

						node, err := etcdClient.Get(`/routes/post_here,1.2.3.4:7000`)
						Expect(err).NotTo(HaveOccurred())
						Expect(node.TTL).To(Equal(uint64(50)))
						Expect(node.Value).To(MatchJSON(`{
							"ip": "1.2.3.4",
							"route": "post_here",
							"port": 7000,
							"ttl": 50,
							"log_guid": "my-guid",
							"route_service_url":"https://my-rs.com"
						}`))
					})
				})

				Context("when an entry already exists", func() {
					BeforeEach(func() {
						route.Route = "next-route"
						route.IP = "9.8.7.6"
						route.Port = 12345

						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())
					})

					It("Updates a route if one already exists", func() {
						route.TTL = 47
						route.LogGuid = "new-guid"

						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())

						node, err := etcdClient.Get(`/routes/next-route,9.8.7.6:12345`)
						Expect(err).NotTo(HaveOccurred())
						Expect(node.TTL).To(Equal(uint64(47)))
						Expect(node.Value).To(MatchJSON(`{
							"ip": "9.8.7.6",
							"route": "next-route",
							"port": 12345,
							"ttl": 47,
							"log_guid": "new-guid"
						}`))
					})
				})
			})

			Describe(".WatchRouteChanges", func() {
				Context("when a route is upserted", func() {
					It("should return an update watch event", func() {
						results, _, _ := etcd.WatchRouteChanges()

						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())

						event := <-results
						Expect(event).NotTo(BeNil())
						Expect(event.Type).To(Equal(storeadapter.UpdateEvent))
					})
				})

				Context("when a route is deleted", func() {
					It("should return an delete watch event", func() {
						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())

						results, _, _ := etcd.WatchRouteChanges()

						err = etcd.DeleteRoute(route)
						Expect(err).NotTo(HaveOccurred())

						event := <-results
						Expect(event).NotTo(BeNil())
						Expect(event.Type).To(Equal(storeadapter.DeleteEvent))
					})
				})

				Context("when a route is expired", func() {
					It("should return an expire watch event", func() {
						route.TTL = 1
						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())
						results, _, _ := etcd.WatchRouteChanges()

						time.Sleep(1 * time.Second)
						event := <-results
						Expect(event).NotTo(BeNil())
						Expect(event.Type).To(Equal(storeadapter.ExpireEvent))
					})
				})
			})

			Describe(".DeleteRoute", func() {
				Context("when a route exists", func() {
					BeforeEach(func() {
						err := etcd.SaveRoute(route)
						Expect(err).NotTo(HaveOccurred())
					})

					It("Deletes the route", func() {
						err := etcd.DeleteRoute(route)
						Expect(err).NotTo(HaveOccurred())
					})
				})

				Context("when deleting a route returns an error", func() {
					It("returns a key not found error if the key does not exists", func() {
						err := etcd.DeleteRoute(route)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(Equal("The specified route could not be found."))
					})
				})
			})
		})

		Describe("Tcp Mappings", func() {
			var (
				tcpMapping db.TcpRouteMapping
			)

			BeforeEach(func() {
				tcpMapping = db.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000)
			})

			Describe("SaveTcpRouteMapping", func() {
				It("Creates a mapping if none exist", func() {
					err := etcd.SaveTcpRouteMapping(tcpMapping)
					Expect(err).NotTo(HaveOccurred())

					key := fmt.Sprintf("%s/%s/%d/%s:%d", db.TCP_MAPPING_BASE_KEY, "router-group-guid-001", 52000, "1.2.3.4", 60000)

					node, err := etcdClient.Get(key)
					Expect(err).NotTo(HaveOccurred())
					Expect(node.Value).To(MatchJSON(`{
							"route": {"router_group_guid":"router-group-guid-001", "external_port":52000},
							"host_ip": "1.2.3.4",
							"host_port": 60000
						}`))
				})
			})

			Describe("ReadTcpRouteMappings", func() {
				It("Returns a empty list of routes", func() {
					tcpMappings, err := etcd.ReadTcpRouteMappings()
					Expect(err).NotTo(HaveOccurred())
					Expect(tcpMappings).To(Equal([]db.TcpRouteMapping{}))
				})

				Context("when only one entry is present", func() {
					BeforeEach(func() {
						err := etcd.SaveTcpRouteMapping(tcpMapping)
						Expect(err).NotTo(HaveOccurred())
					})

					It("Returns a list with one route", func() {
						tcpMappings, err := etcd.ReadTcpRouteMappings()
						Expect(err).NotTo(HaveOccurred())
						Expect(tcpMappings).To(ContainElement(tcpMapping))
					})
				})
			})

		})
	})
})
