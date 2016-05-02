package routing_api_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/models"
	trace "github.com/cloudfoundry-incubator/trace-logger"
	"github.com/onsi/gomega/ghttp"
	"github.com/vito/go-sse/sse"
)

const (
	DefaultRouterGroupGuid = "bad25cff-9332-48a6-8603-b619858e7992"
	DefaultRouterGroupName = "default-tcp"
	DefaultRouterGroupType = "tcp"
)

var _ = Describe("Client", func() {
	const (
		ROUTES_API_URL                    = "/routing/v1/routes"
		TCP_CREATE_ROUTE_MAPPINGS_API_URL = "/routing/v1/tcp_routes/create"
		TCP_DELETE_ROUTE_MAPPINGS_API_URL = "/routing/v1/tcp_routes/delete"
		TCP_ROUTES_API_URL                = "/routing/v1/tcp_routes"
		TCP_ROUTER_GROUPS_API_URL         = "/routing/v1/router_groups"
		EVENTS_SSE_URL                    = "/routing/v1/events"
		TCP_EVENTS_SSE_URL                = "/routing/v1/tcp_routes/events"
	)

	var server *ghttp.Server
	var client routing_api.Client
	var route1 models.Route
	var route2 models.Route
	var stdout *bytes.Buffer

	BeforeEach(func() {
		stdout = bytes.NewBuffer([]byte{})
		trace.SetStdout(stdout)
		trace.Logger = trace.NewLogger("true")
	})

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

		server = ghttp.NewServer()
		client = routing_api.NewClient(server.URL())
	})

	AfterEach(func() {
		server.Close()
	})

	Context("UpsertRoutes", func() {
		var err error
		JustBeforeEach(func() {
			err = client.UpsertRoutes([]models.Route{route1, route2})
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.VerifyRequest("POST", ROUTES_API_URL),
				)
			})

			It("sends an Upsert request to the server", func() {
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})

			It("does not receive an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.Route{route1, route2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("POST " + ROUTES_API_URL + " HTTP/1.1"))
				Expect(log).To(ContainSubstring(string(expectedBody)))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 200 OK"))
			})
		})

		Context("When the server returns an error", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", ROUTES_API_URL),
						ghttp.RespondWith(http.StatusBadRequest, nil),
					),
				)
			})

			It("receives an error", func() {
				Expect(err).To(HaveOccurred())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.Route{route1, route2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("POST " + ROUTES_API_URL + " HTTP/1.1"))
				Expect(log).To(ContainSubstring(string(expectedBody)))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 400 Bad Request"))
			})
		})
	})

	Context("UpsertTcpRouteMappings", func() {

		var (
			err              error
			tcpRouteMapping1 models.TcpRouteMapping
			tcpRouteMapping2 models.TcpRouteMapping
		)
		BeforeEach(func() {
			tcpRouteMapping1 = models.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000, 60)
			tcpRouteMapping2 = models.NewTcpRouteMapping("router-group-guid-001", 52001, "1.2.3.5", 60001, 60)
		})

		JustBeforeEach(func() {
			err = client.UpsertTcpRouteMappings([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.VerifyRequest("POST", TCP_CREATE_ROUTE_MAPPINGS_API_URL),
				)
			})

			It("sends an Upsert request to the server", func() {
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})

			It("does not receive an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("POST " + TCP_CREATE_ROUTE_MAPPINGS_API_URL + " HTTP/1.1"))
				Expect(log).To(ContainSubstring(string(expectedBody)))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 200 OK"))
			})
		})

		Context("when the server returns an error", func() {
			Context("other than unauthorized", func() {
				BeforeEach(func() {
					server.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("POST", TCP_CREATE_ROUTE_MAPPINGS_API_URL),
							ghttp.RespondWith(http.StatusBadRequest, nil),
						),
					)
				})

				It("receives an error", func() {
					Expect(err).To(HaveOccurred())
				})

				It("logs the request and response", func() {
					expectedBody, _ := json.Marshal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

					r, err := ioutil.ReadAll(stdout)
					log := string(r)
					Expect(err).NotTo(HaveOccurred())
					Expect(log).To(ContainSubstring("REQUEST: "))
					Expect(log).To(ContainSubstring("POST " + TCP_CREATE_ROUTE_MAPPINGS_API_URL + " HTTP/1.1"))
					Expect(log).To(ContainSubstring(string(expectedBody)))

					Expect(log).To(ContainSubstring("RESPONSE: "))
					Expect(log).To(ContainSubstring("HTTP/1.1 400 Bad Request"))
				})
			})

			Context("unauthorized", func() {
				BeforeEach(func() {
					server.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("POST", TCP_CREATE_ROUTE_MAPPINGS_API_URL),
							ghttp.RespondWith(http.StatusUnauthorized, nil),
						),
					)
				})

				It("receives an unauthorized error", func() {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).Should(Equal("unauthorized"))
				})

				It("logs the request and response", func() {
					expectedBody, _ := json.Marshal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

					r, err := ioutil.ReadAll(stdout)
					log := string(r)
					Expect(err).NotTo(HaveOccurred())
					Expect(log).To(ContainSubstring("REQUEST: "))
					Expect(log).To(ContainSubstring("POST " + TCP_CREATE_ROUTE_MAPPINGS_API_URL + " HTTP/1.1"))
					Expect(log).To(ContainSubstring(string(expectedBody)))

					Expect(log).To(ContainSubstring("RESPONSE: "))
					Expect(log).To(ContainSubstring("HTTP/1.1 401 Unauthorized"))
				})
			})
		})
	})

	Context("DeleteRoutes", func() {
		var err error
		JustBeforeEach(func() {
			err = client.DeleteRoutes([]models.Route{route1, route2})
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("DELETE", ROUTES_API_URL),
						ghttp.VerifyJSONRepresenting([]models.Route{route1, route2}),
					),
				)
			})

			It("sends a Delete request to the server", func() {
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})

			It("does not receive an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.Route{route1, route2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("DELETE " + ROUTES_API_URL + " HTTP/1.1"))
				Expect(log).To(ContainSubstring(string(expectedBody)))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 200 OK"))
			})
		})

		Context("When the server returns an error", func() {
			Context("other than unauthorized", func() {
				BeforeEach(func() {
					server.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("DELETE", ROUTES_API_URL),
							ghttp.RespondWith(http.StatusBadRequest, nil),
						),
					)
				})

				It("receives an error", func() {
					Expect(err).To(HaveOccurred())
				})

				It("logs the request and response", func() {
					expectedBody, _ := json.Marshal([]models.Route{route1, route2})

					r, err := ioutil.ReadAll(stdout)
					log := string(r)
					Expect(err).NotTo(HaveOccurred())
					Expect(log).To(ContainSubstring("REQUEST: "))
					Expect(log).To(ContainSubstring("DELETE " + ROUTES_API_URL + " HTTP/1.1"))
					Expect(log).To(ContainSubstring(string(expectedBody)))

					Expect(log).To(ContainSubstring("RESPONSE: "))
					Expect(log).To(ContainSubstring("HTTP/1.1 400 Bad Request"))
				})
			})

			Context("unauthorized", func() {
				BeforeEach(func() {
					server.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("DELETE", ROUTES_API_URL),
							ghttp.RespondWith(http.StatusUnauthorized, nil),
						),
					)
				})

				It("receives an unauthorized error", func() {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).Should(Equal("unauthorized"))
				})

				It("logs the request and response", func() {
					expectedBody, _ := json.Marshal([]models.Route{route1, route2})

					r, err := ioutil.ReadAll(stdout)
					log := string(r)
					Expect(err).NotTo(HaveOccurred())
					Expect(log).To(ContainSubstring("REQUEST: "))
					Expect(log).To(ContainSubstring("DELETE " + ROUTES_API_URL + " HTTP/1.1"))
					Expect(log).To(ContainSubstring(string(expectedBody)))

					Expect(log).To(ContainSubstring("RESPONSE: "))
					Expect(log).To(ContainSubstring("HTTP/1.1 401 Unauthorized"))
				})
			})
		})
	})

	Context("DeleteTcpRouteMappings", func() {
		var (
			err              error
			tcpRouteMapping1 models.TcpRouteMapping
			tcpRouteMapping2 models.TcpRouteMapping
		)
		BeforeEach(func() {
			tcpRouteMapping1 = models.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000, 60)
			tcpRouteMapping2 = models.NewTcpRouteMapping("router-group-guid-001", 52001, "1.2.3.5", 60001, 60)
		})
		JustBeforeEach(func() {
			err = client.DeleteTcpRouteMappings([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", TCP_DELETE_ROUTE_MAPPINGS_API_URL),
						func(w http.ResponseWriter, req *http.Request) {
							w.WriteHeader(http.StatusNoContent)
						},
					),
				)
			})

			It("sends a Delete request to the server", func() {
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})

			It("does not receive an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("POST " + TCP_DELETE_ROUTE_MAPPINGS_API_URL + " HTTP/1.1"))
				Expect(log).To(ContainSubstring(string(expectedBody)))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 204 No Content"))
			})
		})

		Context("When the server returns an error", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", TCP_DELETE_ROUTE_MAPPINGS_API_URL),
						ghttp.RespondWith(http.StatusBadRequest, nil),
					),
				)
			})

			It("receives an error", func() {
				Expect(err).To(HaveOccurred())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("POST " + TCP_DELETE_ROUTE_MAPPINGS_API_URL + " HTTP/1.1"))
				Expect(log).To(ContainSubstring(string(expectedBody)))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 400 Bad Request"))
			})
		})
	})

	Context("Routes", func() {
		var routes []models.Route
		var err error

		JustBeforeEach(func() {
			routes, err = client.Routes()
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				data, _ := json.Marshal([]models.Route{route1, route2})

				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", ROUTES_API_URL),
						ghttp.RespondWith(http.StatusOK, data),
					),
				)
			})

			It("Sends a ListRoutes request to the server", func() {
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})

			It("gets a list of routes from the server", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(routes).To(Equal([]models.Route{route1, route2}))
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.Route{route1, route2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("GET " + ROUTES_API_URL + " HTTP/1.1"))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 200 OK"))
				Expect(log).To(ContainSubstring(string(expectedBody)))
			})
		})

		Context("When the server returns an error", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", ROUTES_API_URL),
						ghttp.RespondWith(http.StatusBadRequest, nil),
					),
				)
			})

			It("returns an error", func() {
				Expect(err).To(HaveOccurred())
				Expect(routes).To(BeEmpty())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.Route{route1, route2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("GET " + ROUTES_API_URL + " HTTP/1.1"))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 400 Bad Request"))
				Expect(log).NotTo(ContainSubstring(string(expectedBody)))
			})
		})
	})

	Context("TcpRouteMappings", func() {

		var (
			err              error
			tcpRouteMapping1 models.TcpRouteMapping
			tcpRouteMapping2 models.TcpRouteMapping
			routes           []models.TcpRouteMapping
		)
		BeforeEach(func() {
			tcpRouteMapping1 = models.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000, 60)
			tcpRouteMapping2 = models.NewTcpRouteMapping("router-group-guid-001", 52001, "1.2.3.5", 60001, 60)
		})

		JustBeforeEach(func() {
			routes, err = client.TcpRouteMappings()
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				data, _ := json.Marshal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", TCP_ROUTES_API_URL),
						ghttp.RespondWith(http.StatusOK, data),
					),
				)
			})

			It("Sends a ListRoutes request to the server", func() {
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})

			It("gets a list of routes from the server", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(routes).To(Equal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2}))
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("GET " + TCP_ROUTES_API_URL + " HTTP/1.1"))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 200 OK"))
				Expect(log).To(ContainSubstring(string(expectedBody)))
			})
		})

		Context("When the server returns an error", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", TCP_ROUTES_API_URL),
						ghttp.RespondWith(http.StatusBadRequest, nil),
					),
				)
			})

			It("returns an error", func() {
				Expect(err).To(HaveOccurred())
				Expect(routes).To(BeEmpty())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("GET " + TCP_ROUTES_API_URL + " HTTP/1.1"))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 400 Bad Request"))
				Expect(log).NotTo(ContainSubstring(string(expectedBody)))
			})
		})
	})
	Context("RouterGroups", func() {
		var (
			routerGroups []models.RouterGroup
			err          error
			routerGroup1 models.RouterGroup
		)

		BeforeEach(func() {
			routerGroup1 = models.RouterGroup{
				Guid:            DefaultRouterGroupGuid,
				Name:            DefaultRouterGroupName,
				Type:            DefaultRouterGroupType,
				ReservablePorts: "1024-65535",
			}
		})

		JustBeforeEach(func() {
			routerGroups, err = client.RouterGroups()
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				data, _ := json.Marshal([]models.RouterGroup{routerGroup1})

				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", TCP_ROUTER_GROUPS_API_URL),
						ghttp.RespondWith(http.StatusOK, data),
					),
				)
			})

			It("Sends a ListRouterGroups request to the server", func() {
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})

			It("gets a list of router groups from the server", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(routerGroups).To(Equal([]models.RouterGroup{routerGroup1}))
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.RouterGroup{routerGroup1})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("GET " + TCP_ROUTER_GROUPS_API_URL + " HTTP/1.1"))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 200 OK"))
				Expect(log).To(ContainSubstring(string(expectedBody)))
			})
		})

		Context("When the server returns an error", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", TCP_ROUTER_GROUPS_API_URL),
						ghttp.RespondWith(http.StatusInternalServerError, nil),
					),
				)
			})

			It("returns an error", func() {
				Expect(err).To(HaveOccurred())
				Expect(routerGroups).To(BeEmpty())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]models.RouterGroup{routerGroup1})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("GET " + TCP_ROUTER_GROUPS_API_URL + " HTTP/1.1"))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 500 Internal Server Error"))
				Expect(log).NotTo(ContainSubstring(string(expectedBody)))
			})
		})
	})

	Context("SubscribeToEvents", func() {
		var eventSource routing_api.EventSource
		var err error
		var event sse.Event

		BeforeEach(func() {
			data, _ := json.Marshal(route1)
			event = sse.Event{
				ID:   "1",
				Name: "Upsert",
				Data: data,
			}

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", EVENTS_SSE_URL),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer"},
					}),
					func(w http.ResponseWriter, req *http.Request) {
						event.Write(w)
					},
				),
			)
		})

		JustBeforeEach(func() {
			eventSource, err = client.SubscribeToEvents()
		})

		It("Starts an SSE connection to the server", func() {
			Expect(server.ReceivedRequests()).Should(HaveLen(1))
		})

		It("Streams events from the server", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(eventSource).ToNot(BeNil())

			ev, err := eventSource.Next()
			Expect(err).NotTo(HaveOccurred())

			Expect(ev.Route).To(Equal(route1))
			Expect(ev.Action).To(Equal("Upsert"))
		})

		It("logs the request", func() {
			r, err := ioutil.ReadAll(stdout)
			log := string(r)
			Expect(err).NotTo(HaveOccurred())
			Expect(log).To(ContainSubstring("REQUEST: "))
			Expect(log).To(ContainSubstring("GET " + EVENTS_SSE_URL + " HTTP/1.1"))
		})

		Context("When the server responds with BadResponseError", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", EVENTS_SSE_URL),
						ghttp.RespondWith(http.StatusUnauthorized, nil),
					),
				)
			})

			JustBeforeEach(func() {
				eventSource, err = client.SubscribeToEvents()
			})

			It("propagates the error to the client", func() {
				Expect(err).To(HaveOccurred())
				Expect(eventSource).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("unauthorized"))
			})
		})

		Context("When the server responds with an error", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", EVENTS_SSE_URL),
						ghttp.RespondWith(http.StatusBadRequest, nil),
					),
				)
			})

			JustBeforeEach(func() {
				eventSource, err = client.SubscribeToEvents()
			})

			It("propagates the error to the client", func() {
				Expect(err).To(HaveOccurred())
				Expect(eventSource).To(BeNil())
			})
		})
	})

	Context("SubscribeToEventsWithMaxRetries", func() {
		var (
			retryChannel chan struct{}
		)

		BeforeEach(func() {
			retryChannel = make(chan struct{}, 3)
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", EVENTS_SSE_URL),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer"},
					}),
					func(w http.ResponseWriter, req *http.Request) {
						server.CloseClientConnections()
						retryChannel <- struct{}{}
					},
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", EVENTS_SSE_URL),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer"},
					}),
					func(w http.ResponseWriter, req *http.Request) {
						server.CloseClientConnections()
						retryChannel <- struct{}{}
					},
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", EVENTS_SSE_URL),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer"},
					}),
					func(w http.ResponseWriter, req *http.Request) {
						server.CloseClientConnections()
						retryChannel <- struct{}{}
					},
				),
			)
		})

		It("returns error", func() {
			_, err := client.SubscribeToEventsWithMaxRetries(2)
			Expect(err).To(HaveOccurred())
			Expect(retryChannel).To(Receive())
			Expect(retryChannel).To(Receive())
			Expect(retryChannel).To(Receive())
		})
	})

	Context("SubscribeToTcpEvents", func() {
		var (
			tcpEventSource routing_api.TcpEventSource
			err            error
			event          sse.Event
			tcpRoute1      models.TcpRouteMapping
		)

		BeforeEach(func() {
			tcpRoute1 = models.NewTcpRouteMapping("rguid1", 52000, "1.1.1.1", 60000, 60)

			data, _ := json.Marshal(tcpRoute1)
			event = sse.Event{
				ID:   "1",
				Name: "Upsert",
				Data: data,
			}

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", TCP_EVENTS_SSE_URL),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer"},
					}),
					func(w http.ResponseWriter, req *http.Request) {
						event.Write(w)
					},
				),
			)
		})

		JustBeforeEach(func() {
			tcpEventSource, err = client.SubscribeToTcpEvents()
		})

		It("Starts an SSE connection to the server", func() {
			Expect(server.ReceivedRequests()).Should(HaveLen(1))
		})

		It("Streams events from the server", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(tcpEventSource).ToNot(BeNil())

			ev, err := tcpEventSource.Next()
			Expect(err).NotTo(HaveOccurred())

			Expect(ev.TcpRouteMapping).To(Equal(tcpRoute1))
			Expect(ev.Action).To(Equal("Upsert"))
		})

		It("logs the request", func() {
			r, err := ioutil.ReadAll(stdout)
			log := string(r)
			Expect(err).NotTo(HaveOccurred())
			Expect(log).To(ContainSubstring("REQUEST: "))
			Expect(log).To(ContainSubstring("GET " + TCP_EVENTS_SSE_URL + " HTTP/1.1"))
		})

		Context("When the server responds with an error", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", TCP_EVENTS_SSE_URL),
						ghttp.RespondWith(http.StatusBadRequest, nil),
					),
				)
			})

			JustBeforeEach(func() {
				tcpEventSource, err = client.SubscribeToTcpEvents()
			})

			It("propagates the error to the client", func() {
				Expect(err).To(HaveOccurred())
				Expect(tcpEventSource).To(BeNil())
			})
		})
	})

	Context("SubscribeToTcpEventsWithMaxRetries", func() {
		var (
			retryChannel chan struct{}
		)

		BeforeEach(func() {
			retryChannel = make(chan struct{}, 3)
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", TCP_EVENTS_SSE_URL),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer"},
					}),
					func(w http.ResponseWriter, req *http.Request) {
						server.CloseClientConnections()
						retryChannel <- struct{}{}
					},
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", TCP_EVENTS_SSE_URL),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer"},
					}),
					func(w http.ResponseWriter, req *http.Request) {
						server.CloseClientConnections()
						retryChannel <- struct{}{}
					},
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", TCP_EVENTS_SSE_URL),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer"},
					}),
					func(w http.ResponseWriter, req *http.Request) {
						server.CloseClientConnections()
						retryChannel <- struct{}{}
					},
				),
			)
		})

		It("returns error", func() {
			_, err := client.SubscribeToTcpEventsWithMaxRetries(2)
			Expect(err).To(HaveOccurred())
			Expect(retryChannel).To(Receive())
			Expect(retryChannel).To(Receive())
			Expect(retryChannel).To(Receive())
		})
	})

})
