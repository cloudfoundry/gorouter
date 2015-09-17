package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"

	routing_api "github.com/cloudfoundry-incubator/routing-api"
	fake_token "github.com/cloudfoundry-incubator/routing-api/authentication/fakes"
	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	"github.com/cloudfoundry-incubator/routing-api/handlers"
	fake_validator "github.com/cloudfoundry-incubator/routing-api/handlers/fakes"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RoutesHandler", func() {
	var (
		routesHandler    *handlers.RoutesHandler
		request          *http.Request
		responseRecorder *httptest.ResponseRecorder
		database         *fake_db.FakeDB
		logger           *lagertest.TestLogger
		validator        *fake_validator.FakeRouteValidator
		token            *fake_token.FakeToken
	)

	BeforeEach(func() {
		database = &fake_db.FakeDB{}
		validator = &fake_validator.FakeRouteValidator{}
		token = &fake_token.FakeToken{}
		logger = lagertest.NewTestLogger("routing-api-test")
		routesHandler = handlers.NewRoutesHandler(token, 50, validator, database, logger)
		responseRecorder = httptest.NewRecorder()
	})

	Describe(".List", func() {
		It("response with a 200 OK", func() {
			request = handlers.NewTestRequest("")

			routesHandler.List(responseRecorder, request)

			Expect(responseRecorder.Code).To(Equal(http.StatusOK))
		})

		It("checks for routing.routes.read scope", func() {
			request = handlers.NewTestRequest("")

			routesHandler.List(responseRecorder, request)
			_, permission := token.DecodeTokenArgsForCall(0)
			Expect(permission).To(ConsistOf(handlers.RoutingRoutesReadScope))
		})

		Context("when the UAA token is not valid", func() {
			BeforeEach(func() {
				token.DecodeTokenReturns(errors.New("Not valid"))
			})

			It("returns an Unauthorized status code", func() {
				request = handlers.NewTestRequest("")
				routesHandler.List(responseRecorder, request)
				Expect(responseRecorder.Code).To(Equal(http.StatusUnauthorized))
			})
		})

		Context("when the database is empty", func() {
			var (
				routes []db.Route
			)

			BeforeEach(func() {
				routes = []db.Route{}

				database.ReadRoutesReturns(routes, nil)
			})

			It("returns an empty set", func() {
				request = handlers.NewTestRequest("")

				routesHandler.List(responseRecorder, request)

				Expect(responseRecorder.Body.String()).To(MatchJSON("[]"))
			})
		})

		Context("when the database has one route", func() {
			var (
				routes []db.Route
			)

			BeforeEach(func() {
				routes = []db.Route{
					{
						Route: "post_here",
						IP:    "1.2.3.4",
						Port:  7000,
					},
				}

				database.ReadRoutesReturns(routes, nil)
			})

			It("returns a single route", func() {
				request = handlers.NewTestRequest("")

				routesHandler.List(responseRecorder, request)

				Expect(responseRecorder.Body.String()).To(MatchJSON(`[
							{
								"route": "post_here",
								"port": 7000,
								"ip": "1.2.3.4",
								"ttl": 0,
								"log_guid": ""
							}
						]`))
			})
		})

		Context("when the database has many routes", func() {
			var (
				routes []db.Route
			)

			BeforeEach(func() {
				routes = []db.Route{
					{
						Route: "post_here",
						IP:    "1.2.3.4",
						Port:  7000,
					},
					{
						Route:   "post_there",
						IP:      "1.2.3.5",
						Port:    2000,
						TTL:     23,
						LogGuid: "Something",
					},
				}

				database.ReadRoutesReturns(routes, nil)
			})

			It("returns a single route", func() {
				request = handlers.NewTestRequest("")

				routesHandler.List(responseRecorder, request)

				Expect(responseRecorder.Body.String()).To(MatchJSON(`[
							{
								"route": "post_here",
								"port": 7000,
								"ip": "1.2.3.4",
								"ttl": 0,
								"log_guid": ""
							},
							{
								"route": "post_there",
								"port": 2000,
								"ip": "1.2.3.5",
								"ttl": 23,
								"log_guid": "Something"
							}
						]`))
			})
		})

		Context("when the database errors out", func() {
			BeforeEach(func() {
				database.ReadRoutesReturns(nil, errors.New("some bad thing happened"))
			})

			It("returns a 500 Internal Server Error", func() {
				request = handlers.NewTestRequest("")

				routesHandler.List(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe(".DeleteRoute", func() {
		var (
			route []db.Route
		)

		BeforeEach(func() {
			route = []db.Route{
				{
					Route: "post_here",
					IP:    "1.2.3.4",
					Port:  7000,
				},
			}
		})

		It("checks for routing.routes.write scope", func() {
			request = handlers.NewTestRequest(route)

			routesHandler.Delete(responseRecorder, request)

			_, permission := token.DecodeTokenArgsForCall(0)
			Expect(permission).To(ConsistOf(handlers.RoutingRoutesWriteScope))
		})

		Context("when all inputs are present and correct", func() {
			It("returns a status not found when deleting a route", func() {
				request = handlers.NewTestRequest(route)

				routesHandler.Delete(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
				Expect(database.DeleteRouteCallCount()).To(Equal(1))
				Expect(database.DeleteRouteArgsForCall(0)).To(Equal(route[0]))
			})

			It("accepts an array of routes in the body", func() {
				route = append(route, route[0])
				route[1].IP = "5.4.3.2"

				request = handlers.NewTestRequest(route)
				routesHandler.Delete(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
				Expect(database.DeleteRouteCallCount()).To(Equal(2))
				Expect(database.DeleteRouteArgsForCall(0)).To(Equal(route[0]))
				Expect(database.DeleteRouteArgsForCall(1)).To(Equal(route[1]))
			})

			It("logs the route deletion", func() {
				request = handlers.NewTestRequest(route)
				routesHandler.Delete(responseRecorder, request)

				data := map[string]interface{}{
					"ip":       "1.2.3.4",
					"log_guid": "",
					"port":     float64(7000),
					"route":    "post_here",
					"ttl":      float64(0),
				}
				log_data := map[string][]interface{}{"route_deletion": []interface{}{data}}

				Expect(logger.Logs()[0].Message).To(ContainSubstring("request"))
				Expect(logger.Logs()[0].Data["route_deletion"]).To(Equal(log_data["route_deletion"]))
			})

			Context("when the database deletion fails", func() {
				It("returns a 204 if the key was not found", func() {
					database.DeleteRouteReturns(db.DBError{Type: db.KeyNotFound, Message: "The specified route could not be found."})

					request = handlers.NewTestRequest(route)
					routesHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
				})

				It("responds with a server error", func() {
					database.DeleteRouteReturns(errors.New("stuff broke"))

					request = handlers.NewTestRequest(route)
					routesHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("stuff broke"))
				})
			})
		})

		Context("when there are errors with the input", func() {
			It("returns a bad request if it cannot parse the arguments", func() {
				request = handlers.NewTestRequest("bad args")

				routesHandler.Delete(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
				Expect(responseRecorder.Body.String()).To(ContainSubstring("Cannot process request"))
			})
		})

		Context("when the UAA token is not valid", func() {
			BeforeEach(func() {
				token.DecodeTokenReturns(errors.New("Not valid"))
			})

			It("returns an Unauthorized status code", func() {
				request = handlers.NewTestRequest(route)
				routesHandler.Delete(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusUnauthorized))
			})
		})
	})

	Describe(".Upsert", func() {
		Context("POST", func() {
			var (
				route []db.Route
			)

			BeforeEach(func() {
				route = []db.Route{
					{
						Route: "post_here",
						IP:    "1.2.3.4",
						Port:  7000,
						TTL:   50,
					},
				}
			})

			It("checks for routing.routes.write scope", func() {
				request = handlers.NewTestRequest(route)

				routesHandler.Upsert(responseRecorder, request)

				_, permission := token.DecodeTokenArgsForCall(0)
				Expect(permission).To(ConsistOf(handlers.RoutingRoutesWriteScope))
			})

			Context("when all inputs are present and correct", func() {
				It("returns an http status created", func() {
					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
				})

				It("accepts a list of routes in the body", func() {
					route = append(route, route[0])
					route[1].IP = "5.4.3.2"

					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
					Expect(database.SaveRouteCallCount()).To(Equal(2))
					Expect(database.SaveRouteArgsForCall(0)).To(Equal(route[0]))
					Expect(database.SaveRouteArgsForCall(1)).To(Equal(route[1]))
				})

				It("accepts route_service_url parameters", func() {
					route[0].RouteServiceUrl = "https://my-rs.com"
					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
					Expect(database.SaveRouteCallCount()).To(Equal(1))
					Expect(database.SaveRouteArgsForCall(0)).To(Equal(route[0]))
				})

				It("logs the route declaration", func() {
					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					data := map[string]interface{}{
						"ip":       "1.2.3.4",
						"log_guid": "",
						"port":     float64(7000),
						"route":    "post_here",
						"ttl":      float64(50),
					}
					log_data := map[string][]interface{}{"route_creation": []interface{}{data}}

					Expect(logger.Logs()[0].Message).To(ContainSubstring("request"))
					Expect(logger.Logs()[0].Data["route_creation"]).To(Equal(log_data["route_creation"]))
				})

				It("does not require route_service_url on the request", func() {
					route[0].RouteServiceUrl = ""

					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
				})

				It("does not require log guid on the request", func() {
					route[0].LogGuid = ""

					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
				})

				It("writes to database backend", func() {
					route[0].LogGuid = "my-guid"

					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(database.SaveRouteCallCount()).To(Equal(1))
					Expect(database.SaveRouteArgsForCall(0)).To(Equal(route[0]))
				})

				Context("when database fails to save", func() {
					BeforeEach(func() {
						database.SaveRouteReturns(errors.New("stuff broke"))
					})

					It("responds with a server error", func() {
						request = handlers.NewTestRequest(route)
						routesHandler.Upsert(responseRecorder, request)

						Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
						Expect(responseRecorder.Body.String()).To(ContainSubstring("stuff broke"))
					})
				})
			})

			Context("when there are errors with the input", func() {
				BeforeEach(func() {
					validator.ValidateCreateReturns(&routing_api.Error{Type: "a type", Message: "error message"})
				})

				It("blows up when a port does not fit into a uint16", func() {
					json := `[{"route":"my-route.com","ip":"1.2.3.4", "port":65537}]`
					request = handlers.NewTestRequest(json)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number 65537 into Go value of type uint16"))
				})

				It("does not write to the key-value store backend", func() {
					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(database.SaveRouteCallCount()).To(Equal(0))
				})

				It("logs the error", func() {
					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(logger.Logs()[1].Message).To(ContainSubstring("error"))
					Expect(logger.Logs()[1].Data["error"]).To(Equal("error message"))
				})
			})

			Context("when the UAA token is not valid", func() {
				BeforeEach(func() {
					token.DecodeTokenReturns(errors.New("Not valid"))
				})

				It("returns an Unauthorized status code", func() {
					request = handlers.NewTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusUnauthorized))
				})
			})
		})
	})
})
