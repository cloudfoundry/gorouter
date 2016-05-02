package handlers_test

import (
	"errors"
	// "fmt"
	"net/http"
	"net/http/httptest"

	routing_api "github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	fake_validator "github.com/cloudfoundry-incubator/routing-api/handlers/fakes"
	"github.com/cloudfoundry-incubator/routing-api/metrics"
	"github.com/cloudfoundry-incubator/routing-api/models"
	fake_client "github.com/cloudfoundry-incubator/uaa-go-client/fakes"
	"github.com/pivotal-golang/lager/lagertest"

	"github.com/cloudfoundry-incubator/routing-api/handlers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func expectInvalidInput(responseRecorder *httptest.ResponseRecorder, database *fake_db.FakeDB, logger *lagertest.TestLogger) {
	Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
	Expect(responseRecorder.Body.String()).To(ContainSubstring("Each tcp mapping requires a positive host port"))
	Expect(database.SaveRouteCallCount()).To(Equal(0))
	Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))
}

var _ = Describe("TcpRouteMappingsHandler", func() {
	var (
		tcpRouteMappingsHandler *handlers.TcpRouteMappingsHandler
		request                 *http.Request
		responseRecorder        *httptest.ResponseRecorder
		validator               *fake_validator.FakeRouteValidator
		database                *fake_db.FakeDB
		logger                  *lagertest.TestLogger
		fakeClient              *fake_client.FakeClient
	)

	BeforeEach(func() {
		database = &fake_db.FakeDB{}
		fakeClient = &fake_client.FakeClient{}
		validator = &fake_validator.FakeRouteValidator{}
		logger = lagertest.NewTestLogger("routing-api-test")
		tcpRouteMappingsHandler = handlers.NewTcpRouteMappingsHandler(fakeClient, validator, database, 120, logger)
		responseRecorder = httptest.NewRecorder()
	})

	Describe("Upsert", func() {
		Context("POST", func() {
			var (
				tcpMapping  models.TcpRouteMapping
				tcpMappings []models.TcpRouteMapping
			)

			BeforeEach(func() {
				tcpMapping = models.TcpRouteMapping{
					TcpRoute: models.TcpRoute{
						RouterGroupGuid: "router-group-guid-001",
						ExternalPort:    52000,
					},
					HostIP:   "1.2.3.4",
					HostPort: 60000,
				}
				tcpMappings = []models.TcpRouteMapping{tcpMapping}
			})

			It("checks for routing.routes.write scope", func() {
				request = handlers.NewTestRequest(tcpMappings)

				tcpRouteMappingsHandler.Upsert(responseRecorder, request)
				Expect(responseRecorder.Code).To(Equal(http.StatusCreated))

				_, permission := fakeClient.DecodeTokenArgsForCall(0)
				Expect(permission).To(ConsistOf(handlers.RoutingRoutesWriteScope))
			})

			Context("when all inputs are present and correct", func() {
				It("returns an http status created", func() {
					request = handlers.NewTestRequest(tcpMappings)
					tcpRouteMappingsHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
				})

				It("accepts a list of routes in the body", func() {
					tcpMappings = append(tcpMappings, tcpMappings[0])
					tcpMappings[1].HostIP = "5.4.3.2"

					request = handlers.NewTestRequest(tcpMappings)
					tcpRouteMappingsHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusCreated))
					Expect(database.SaveTcpRouteMappingCallCount()).To(Equal(2))
					Expect(database.SaveTcpRouteMappingArgsForCall(0)).To(Equal(tcpMappings[0]))
					Expect(database.SaveTcpRouteMappingArgsForCall(1)).To(Equal(tcpMappings[1]))
				})

				It("logs the route declaration", func() {
					request = handlers.NewTestRequest(tcpMappings)
					tcpRouteMappingsHandler.Upsert(responseRecorder, request)

					data := map[string]interface{}{
						"port":              float64(52000),
						"router_group_guid": "router-group-guid-001",
						"backend_ip":        "1.2.3.4",
						"backend_port":      float64(60000),
						"modification_tag":  map[string]interface{}{"guid": "", "index": float64(0)},
						"ttl":               float64(0),
					}
					log_data := map[string][]interface{}{"tcp_mapping_creation": []interface{}{data}}

					Expect(logger.Logs()[0].Message).To(ContainSubstring("request"))
					Expect(logger.Logs()[0].Data["tcp_mapping_creation"]).To(Equal(log_data["tcp_mapping_creation"]))
				})

				Context("when database fails to save", func() {
					BeforeEach(func() {
						database.SaveTcpRouteMappingReturns(errors.New("stuff broke"))
					})

					It("responds with a server error", func() {
						request = handlers.NewTestRequest(tcpMappings)
						tcpRouteMappingsHandler.Upsert(responseRecorder, request)

						Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
						Expect(responseRecorder.Body.String()).To(ContainSubstring("stuff broke"))
					})
				})

				Context("when conflict error is returned", func() {
					BeforeEach(func() {
						database.SaveTcpRouteMappingReturns(db.ErrorConflict)
					})

					It("responds with a 409 conflict error", func() {
						request = handlers.NewTestRequest(tcpMappings)
						tcpRouteMappingsHandler.Upsert(responseRecorder, request)

						Expect(responseRecorder.Code).To(Equal(http.StatusConflict))
						Expect(responseRecorder.Body.String()).To(ContainSubstring("DBConflictError"))
					})
				})
			})

			Context("when there are errors with the input ports", func() {
				It("blows up when a external port is negative", func() {
					request = handlers.NewTestRequest(`[{"router_group_guid": "tcp-default", "port": -1, "backend_ip": "10.1.1.12", "backend_port": 60000}]`)
					tcpRouteMappingsHandler.Upsert(responseRecorder, request)
					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number -1 into Go value of type uint16"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))
				})

				It("blows up when a external port does not fit into a uint16", func() {
					request = handlers.NewTestRequest(`[{"router_group_guid": "tcp-default", "port": 65537, "backend_ip": "10.1.1.12", "backend_port": 60000}]`)

					tcpRouteMappingsHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number 65537 into Go value of type uint16"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))
				})

				It("blows up when a host port is negative", func() {
					request = handlers.NewTestRequest(`[{"router_group_guid": "tcp-default", "port": 52000, "backend_ip": "10.1.1.12", "backend_port": -1}]`)
					tcpRouteMappingsHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number -1 into Go value of type uint16"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))

				})

				It("blows up when a host port does not fit into a uint16", func() {
					request = handlers.NewTestRequest(`[{"router_group_guid": "tcp-default", "port": 5200, "backend_ip": "10.1.1.12", "backend_port": 65537}]`)

					tcpRouteMappingsHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number 65537 into Go value of type uint16"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))

				})
			})

			Context("when validator returns error", func() {
				BeforeEach(func() {
					err := routing_api.NewError(routing_api.TcpRouteMappingInvalidError, "Each tcp mapping requires a valid router group guid")
					validator.ValidateCreateTcpRouteMappingReturns(&err)
				})

				It("returns error", func() {
					request = handlers.NewTestRequest(`[{"route":{"router_group_guid": "", "port": 52000}, "backend_ip": "10.1.1.12", "backend_port": 60000}]`)
					tcpRouteMappingsHandler.Upsert(responseRecorder, request)
					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("Each tcp mapping requires a valid router group guid"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[1].Message).To(ContainSubstring("error"))
				})
			})

			Context("when the UAA token is not valid", func() {
				var (
					currentCount int64
				)
				BeforeEach(func() {
					currentCount = metrics.GetTokenErrors()
					fakeClient.DecodeTokenReturns(errors.New("Not valid"))
				})

				It("returns an Unauthorized status code", func() {
					request = handlers.NewTestRequest(tcpMappings)
					tcpRouteMappingsHandler.Upsert(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusUnauthorized))
					Expect(metrics.GetTokenErrors()).To(Equal(currentCount + 1))
				})
			})
		})
	})

	Describe("List", func() {

		It("checks for routing.routes.read scope", func() {
			request = handlers.NewTestRequest("")

			tcpRouteMappingsHandler.List(responseRecorder, request)
			Expect(responseRecorder.Code).To(Equal(http.StatusOK))

			_, permission := fakeClient.DecodeTokenArgsForCall(0)
			Expect(permission).To(ConsistOf(handlers.RoutingRoutesReadScope))
		})

		Context("when db returns tcp route mappings", func() {
			var (
				tcpRoutes []models.TcpRouteMapping
			)

			BeforeEach(func() {
				mapping1 := models.TcpRouteMapping{
					TcpRoute: models.TcpRoute{
						RouterGroupGuid: "router-group-guid-001",
						ExternalPort:    52000,
					},
					HostIP:   "1.2.3.4",
					HostPort: 60000,
					TTL:      55,
				}
				mapping2 := models.TcpRouteMapping{
					TcpRoute: models.TcpRoute{
						RouterGroupGuid: "router-group-guid-001",
						ExternalPort:    52001,
					},
					HostIP:   "1.2.3.5",
					HostPort: 60001,
					TTL:      55,
				}
				tcpRoutes = []models.TcpRouteMapping{mapping1, mapping2}
				database.ReadTcpRouteMappingsReturns(tcpRoutes, nil)
			})

			It("returns tcp route mappings", func() {
				request = handlers.NewTestRequest("")
				tcpRouteMappingsHandler.List(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusOK))
				expectedJson := `[
							{
								"router_group_guid": "router-group-guid-001",
								"port": 52000,
								"backend_ip": "1.2.3.4",
								"backend_port": 60000,
								"modification_tag": {
									"guid": "",
									"index": 0
								},
								"ttl": 55
							},
							{
								"router_group_guid": "router-group-guid-001",
								"port": 52001,
								"backend_ip": "1.2.3.5",
								"backend_port": 60001,
								"modification_tag": {
									"guid": "",
									"index": 0
								},
								"ttl": 55
							}]`
				Expect(responseRecorder.Body.String()).To(MatchJSON(expectedJson))
			})
		})

		Context("when db returns empty tcp route mappings", func() {
			BeforeEach(func() {
				database.ReadTcpRouteMappingsReturns([]models.TcpRouteMapping{}, nil)
			})

			It("returns empty response", func() {
				request = handlers.NewTestRequest("")
				tcpRouteMappingsHandler.List(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusOK))
				Expect(responseRecorder.Body.String()).To(MatchJSON(`[]`))
			})
		})

		Context("when db returns error", func() {
			BeforeEach(func() {
				database.ReadTcpRouteMappingsReturns(nil, errors.New("something bad"))
			})
			It("returns internal server error", func() {
				request = handlers.NewTestRequest("")
				tcpRouteMappingsHandler.List(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
			})
		})

		Context("when the UAA token is not valid", func() {
			var (
				currentCount int64
			)
			BeforeEach(func() {
				currentCount = metrics.GetTokenErrors()
				fakeClient.DecodeTokenReturns(errors.New("Not valid"))
			})

			It("returns an Unauthorized status code", func() {
				request = handlers.NewTestRequest("")
				tcpRouteMappingsHandler.List(responseRecorder, request)

				Expect(responseRecorder.Code).To(Equal(http.StatusUnauthorized))
				Expect(metrics.GetTokenErrors()).To(Equal(currentCount + 1))
			})
		})

	})

	Describe("Delete", func() {
		Context("POST", func() {
			var (
				tcpMapping  models.TcpRouteMapping
				tcpMappings []models.TcpRouteMapping
			)

			BeforeEach(func() {

				tcpMapping = models.TcpRouteMapping{
					TcpRoute: models.TcpRoute{
						RouterGroupGuid: "router-group-guid-002",
						ExternalPort:    52001,
					},
					HostIP:   "1.2.3.4",
					HostPort: 60000,
				}
				tcpMappings = []models.TcpRouteMapping{tcpMapping}
			})

			It("checks for routing.routes.write scope", func() {
				request = handlers.NewTestRequest(tcpMappings)

				tcpRouteMappingsHandler.Delete(responseRecorder, request)
				Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))

				_, permission := fakeClient.DecodeTokenArgsForCall(0)
				Expect(permission).To(ConsistOf(handlers.RoutingRoutesWriteScope))
			})

			Context("when all inputs are present and correct", func() {
				It("returns an http status no content", func() {
					request = handlers.NewTestRequest(tcpMappings)
					tcpRouteMappingsHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
				})

				It("accepts a list of routes in the body", func() {
					tcpMappings = append(tcpMappings, tcpMappings[0])
					tcpMappings[1].HostIP = "5.4.3.2"

					request = handlers.NewTestRequest(tcpMappings)
					tcpRouteMappingsHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
					Expect(database.DeleteTcpRouteMappingCallCount()).To(Equal(2))
					Expect(database.DeleteTcpRouteMappingArgsForCall(0)).To(Equal(tcpMappings[0]))
					Expect(database.DeleteTcpRouteMappingArgsForCall(1)).To(Equal(tcpMappings[1]))
				})

				It("logs the route deletion", func() {
					request = handlers.NewTestRequest(tcpMappings)
					tcpRouteMappingsHandler.Delete(responseRecorder, request)

					data := map[string]interface{}{
						"port":              float64(52001),
						"router_group_guid": "router-group-guid-002",
						"backend_ip":        "1.2.3.4",
						"backend_port":      float64(60000),
						"modification_tag":  map[string]interface{}{"guid": "", "index": float64(0)},
						"ttl":               float64(0),
					}
					log_data := map[string][]interface{}{"tcp_mapping_deletion": []interface{}{data}}

					Expect(logger.Logs()[0].Message).To(ContainSubstring("request"))
					Expect(logger.Logs()[0].Data["tcp_mapping_deletion"]).To(Equal(log_data["tcp_mapping_deletion"]))
				})

				Context("when database fails to delete", func() {
					BeforeEach(func() {
						database.DeleteTcpRouteMappingReturns(errors.New("stuff broke"))
					})
					It("responds with a server error", func() {
						request = handlers.NewTestRequest(tcpMappings)
						tcpRouteMappingsHandler.Delete(responseRecorder, request)

						Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
						Expect(responseRecorder.Body.String()).To(ContainSubstring("stuff broke"))
					})
				})

				Context("when route to be deleted is not present", func() {
					BeforeEach(func() {
						database.DeleteTcpRouteMappingReturns(db.DBError{Type: db.KeyNotFound, Message: "The specified key is not found"})
					})
					It("doesn't fail", func() {
						request = handlers.NewTestRequest(tcpMappings)
						tcpRouteMappingsHandler.Delete(responseRecorder, request)

						Expect(responseRecorder.Code).To(Equal(http.StatusNoContent))
					})
				})
			})

			Context("when there are errors with the input ports", func() {

				It("blows up when a external port is negative", func() {
					request = handlers.NewTestRequest(`[{"router_group_guid": "tcp-default", "port": -1, "backend_ip": "10.1.1.12", "backend_port": 60000}]`)
					tcpRouteMappingsHandler.Delete(responseRecorder, request)
					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number -1 into Go value of type uint16"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))
				})

				It("blows up when a external port does not fit into a uint16", func() {
					request = handlers.NewTestRequest(`[{"router_group_guid": "tcp-default", "port": 65537, "backend_ip": "10.1.1.12", "backend_port": 60000}]`)

					tcpRouteMappingsHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number 65537 into Go value of type uint16"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))
				})

				It("blows up when a host port is negative", func() {
					request = handlers.NewTestRequest(`[{"router_group_guid": "tcp-default", "port": 52000, "backend_ip": "10.1.1.12", "backend_port": -1}]`)
					tcpRouteMappingsHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number -1 into Go value of type uint16"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))

				})

				It("blows up when a host port does not fit into a uint16", func() {
					request = handlers.NewTestRequest(`[{"router_group_guid": "tcp-default", "port": 5200, "backend_ip": "10.1.1.12", "backend_port": 65537}]`)

					tcpRouteMappingsHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("cannot unmarshal number 65537 into Go value of type uint16"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[0].Message).To(ContainSubstring("error"))

				})

			})

			Context("when validator returns error", func() {
				BeforeEach(func() {
					err := routing_api.NewError(routing_api.TcpRouteMappingInvalidError, "Each tcp mapping requires a valid router group guid")
					validator.ValidateDeleteTcpRouteMappingReturns(&err)
				})

				It("returns error", func() {
					request = handlers.NewTestRequest(`[{"route":{"router_group_guid": "", "port": 52000}, "backend_ip": "10.1.1.12", "backend_port": 60000}]`)
					tcpRouteMappingsHandler.Delete(responseRecorder, request)
					Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
					Expect(responseRecorder.Body.String()).To(ContainSubstring("Each tcp mapping requires a valid router group guid"))
					Expect(database.SaveRouteCallCount()).To(Equal(0))
					Expect(logger.Logs()[1].Message).To(ContainSubstring("error"))
				})
			})

			Context("when the UAA token is not valid", func() {
				var (
					currentCount int64
				)
				BeforeEach(func() {
					currentCount = metrics.GetTokenErrors()
					fakeClient.DecodeTokenReturns(errors.New("Not valid"))
				})

				It("returns an Unauthorized status code", func() {
					request = handlers.NewTestRequest(tcpMappings)
					tcpRouteMappingsHandler.Delete(responseRecorder, request)

					Expect(responseRecorder.Code).To(Equal(http.StatusUnauthorized))
					Expect(metrics.GetTokenErrors()).To(Equal(currentCount + 1))
				})
			})
		})
	})

})
