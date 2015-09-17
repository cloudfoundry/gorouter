package routing_api_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/helpers"
	trace "github.com/cloudfoundry-incubator/trace-logger"
	"github.com/onsi/gomega/ghttp"
	"github.com/vito/go-sse/sse"
)

var _ = Describe("Client", func() {
	const (
		ROUTES_API_URL            = "/routing/v1/routes"
		TCP_CREATE_ROUTES_API_URL = "/routing/v1/tcp_routes/create"
		TCP_ROUTES_API_URL        = "/routing/v1/tcp_routes"
		TCP_ROUTER_GROUPS_API_URL = "/routing/v1/router_groups"
		EVENTS_SSE_URL            = "/routing/v1/events"
	)

	var server *ghttp.Server
	var client routing_api.Client
	var route1 db.Route
	var route2 db.Route
	var stdout *bytes.Buffer

	BeforeEach(func() {
		stdout = bytes.NewBuffer([]byte{})
		trace.SetStdout(stdout)
		trace.Logger = trace.NewLogger("true")
	})

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

		server = ghttp.NewServer()
		client = routing_api.NewClient(server.URL())
	})

	AfterEach(func() {
		server.Close()
	})

	Context("UpsertRoutes", func() {
		var err error
		JustBeforeEach(func() {
			err = client.UpsertRoutes([]db.Route{route1, route2})
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
				expectedBody, _ := json.Marshal([]db.Route{route1, route2})

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
				expectedBody, _ := json.Marshal([]db.Route{route1, route2})

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
			tcpRouteMapping1 db.TcpRouteMapping
			tcpRouteMapping2 db.TcpRouteMapping
		)
		BeforeEach(func() {
			tcpRouteMapping1 = db.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000)
			tcpRouteMapping2 = db.NewTcpRouteMapping("router-group-guid-001", 52001, "1.2.3.5", 60001)
		})

		JustBeforeEach(func() {
			err = client.UpsertTcpRouteMappings([]db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.VerifyRequest("POST", TCP_CREATE_ROUTES_API_URL),
				)
			})

			It("sends an Upsert request to the server", func() {
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})

			It("does not receive an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("POST " + TCP_CREATE_ROUTES_API_URL + " HTTP/1.1"))
				Expect(log).To(ContainSubstring(string(expectedBody)))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 200 OK"))
			})
		})

		Context("When the server returns an error", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", TCP_CREATE_ROUTES_API_URL),
						ghttp.RespondWith(http.StatusBadRequest, nil),
					),
				)
			})

			It("receives an error", func() {
				Expect(err).To(HaveOccurred())
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

				r, err := ioutil.ReadAll(stdout)
				log := string(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("REQUEST: "))
				Expect(log).To(ContainSubstring("POST " + TCP_CREATE_ROUTES_API_URL + " HTTP/1.1"))
				Expect(log).To(ContainSubstring(string(expectedBody)))

				Expect(log).To(ContainSubstring("RESPONSE: "))
				Expect(log).To(ContainSubstring("HTTP/1.1 400 Bad Request"))
			})
		})
	})

	Context("DeleteRoutes", func() {
		var err error
		JustBeforeEach(func() {
			err = client.DeleteRoutes([]db.Route{route1, route2})
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("DELETE", ROUTES_API_URL),
						ghttp.VerifyJSONRepresenting([]db.Route{route1, route2}),
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
				expectedBody, _ := json.Marshal([]db.Route{route1, route2})

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
				expectedBody, _ := json.Marshal([]db.Route{route1, route2})

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
	})

	Context("Routes", func() {
		var routes []db.Route
		var err error

		JustBeforeEach(func() {
			routes, err = client.Routes()
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				data, _ := json.Marshal([]db.Route{route1, route2})

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
				Expect(routes).To(Equal([]db.Route{route1, route2}))
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]db.Route{route1, route2})

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
				expectedBody, _ := json.Marshal([]db.Route{route1, route2})

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
			tcpRouteMapping1 db.TcpRouteMapping
			tcpRouteMapping2 db.TcpRouteMapping
			routes           []db.TcpRouteMapping
		)
		BeforeEach(func() {
			tcpRouteMapping1 = db.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000)
			tcpRouteMapping2 = db.NewTcpRouteMapping("router-group-guid-001", 52001, "1.2.3.5", 60001)
		})

		JustBeforeEach(func() {
			routes, err = client.TcpRouteMappings()
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				data, _ := json.Marshal([]db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

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
				Expect(routes).To(Equal([]db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2}))
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

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
				expectedBody, _ := json.Marshal([]db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})

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
			routerGroups []db.RouterGroup
			err          error
			routerGroup1 db.RouterGroup
		)

		BeforeEach(func() {
			routerGroup1 = helpers.GetDefaultRouterGroup()
		})

		JustBeforeEach(func() {
			routerGroups, err = client.RouterGroups()
		})

		Context("when the server returns a valid response", func() {
			BeforeEach(func() {
				data, _ := json.Marshal([]db.RouterGroup{routerGroup1})

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
				Expect(routerGroups).To(Equal([]db.RouterGroup{routerGroup1}))
			})

			It("logs the request and response", func() {
				expectedBody, _ := json.Marshal([]db.RouterGroup{routerGroup1})

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
				expectedBody, _ := json.Marshal([]db.RouterGroup{routerGroup1})

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
})
