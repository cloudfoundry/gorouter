package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/cloudfoundry-incubator/routing-api"
	fake_token "github.com/cloudfoundry-incubator/routing-api/authentication/fakes"
	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	"github.com/cloudfoundry-incubator/routing-api/handlers"
	fake_validator "github.com/cloudfoundry-incubator/routing-api/handlers/fakes"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func newTestRequest(body interface{}) *http.Request {
	var reader io.Reader
	switch body := body.(type) {

	case string:
		reader = strings.NewReader(body)
	case []byte:
		reader = bytes.NewReader(body)
	default:
		jsonBytes, err := json.Marshal(body)
		Ω(err).ToNot(HaveOccurred())
		reader = bytes.NewReader(jsonBytes)
	}

	request, err := http.NewRequest("", "", reader)
	Ω(err).ToNot(HaveOccurred())
	return request
}

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
			request = newTestRequest("")

			routesHandler.List(responseRecorder, request)

			Expect(responseRecorder.Code).To(Equal(http.StatusOK))
		})

		It("checks for route.admin scope", func() {
			request = newTestRequest("")

			routesHandler.List(responseRecorder, request)
			_, permission := token.DecodeTokenArgsForCall(0)
			Expect(permission).To(ConsistOf(handlers.AdminRouteScope))
		})

		Context("when the UAA token is not valid", func() {
			BeforeEach(func() {
				token.DecodeTokenReturns(errors.New("Not valid"))
			})

			It("returns an Unauthorized status code", func() {
				request = newTestRequest("")
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
				request = newTestRequest("")

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
				request = newTestRequest("")

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
				request = newTestRequest("")

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
				request = newTestRequest("")

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

		It("checks for route.advertise & route.admin scope", func() {
			request = newTestRequest(route)

			routesHandler.Delete(responseRecorder, request)

			_, permission := token.DecodeTokenArgsForCall(0)
			Expect(permission).To(ConsistOf(handlers.AdvertiseRouteScope, handlers.AdminRouteScope))
		})

		Context("when all inputs are present and correct", func() {
			It("returns a status not found when deleting a route", func() {
				request = newTestRequest(route)

				routesHandler.Delete(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
				Expect(database.DeleteRouteCallCount()).To(Equal(1))
				Expect(database.DeleteRouteArgsForCall(0)).To(Equal(route[0]))
			})

			It("accepts an array of routes in the body", func() {
				route = append(route, route[0])
				route[1].IP = "5.4.3.2"

				request = newTestRequest(route)
				routesHandler.Delete(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
				Expect(database.DeleteRouteCallCount()).To(Equal(2))
				Expect(database.DeleteRouteArgsForCall(0)).To(Equal(route[0]))
				Expect(database.DeleteRouteArgsForCall(1)).To(Equal(route[1]))
			})

			It("logs the route deletion", func() {
				request = newTestRequest(route)
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
					database.DeleteRouteReturns(errors.New("Key not found"))

					request = newTestRequest(route)
					routesHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
				})

				It("responds with a server error", func() {
					database.DeleteRouteReturns(errors.New("stuff broke"))

					request = newTestRequest(route)
					routesHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("stuff broke"))
				})
			})
		})

		Context("when there are errors with the input", func() {
			It("returns a bad request if it cannot parse the arguments", func() {
				request = newTestRequest("bad args")

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
				request = newTestRequest(route)
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

			It("checks for route.advertise & route.admin scope", func() {
				request = newTestRequest(route)

				routesHandler.Upsert(responseRecorder, request)

				_, permission := token.DecodeTokenArgsForCall(0)
				Expect(permission).To(ConsistOf(handlers.AdvertiseRouteScope, handlers.AdminRouteScope))
			})

			Context("when all inputs are present and correct", func() {
				It("returns an http status created", func() {
					request = newTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
				})

				It("accepts a list of routes in the body", func() {
					route = append(route, route[0])
					route[1].IP = "5.4.3.2"

					request = newTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
					Expect(database.SaveRouteCallCount()).To(Equal(2))
					Expect(database.SaveRouteArgsForCall(0)).To(Equal(route[0]))
					Expect(database.SaveRouteArgsForCall(1)).To(Equal(route[1]))
				})

				It("logs the route declaration", func() {
					request = newTestRequest(route)
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

				It("does not require log guid on the request", func() {
					route[0].LogGuid = ""

					request = newTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
				})

				It("writes to database backend", func() {
					route[0].LogGuid = "my-guid"

					request = newTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(database.SaveRouteCallCount()).To(Equal(1))
					Expect(database.SaveRouteArgsForCall(0)).To(Equal(route[0]))
				})

				Context("when database fails to save", func() {
					BeforeEach(func() {
						database.SaveRouteReturns(errors.New("stuff broke"))
					})

					It("responds with a server error", func() {
						request = newTestRequest(route)
						routesHandler.Upsert(responseRecorder, request)

						Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
						Expect(responseRecorder.Body.String()).To(ContainSubstring("stuff broke"))
					})
				})
			})

			Context("when there are errors with the input", func() {
				BeforeEach(func() {
					validator.ValidateCreateReturns(&routing_api.Error{"a type", "error message"})
				})

				It("does not write to the key-value store backend", func() {
					request = newTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(database.SaveRouteCallCount()).To(Equal(0))
				})

				It("logs the error", func() {
					request = newTestRequest(route)
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
					request = newTestRequest(route)
					routesHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusUnauthorized))
				})
			})
		})
	})
})
