package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/cloudfoundry-incubator/routing-api"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	"github.com/cloudfoundry-incubator/routing-api/handlers"
	"github.com/cloudfoundry-incubator/routing-api/metrics"
	"github.com/cloudfoundry-incubator/routing-api/models"
	fake_client "github.com/cloudfoundry-incubator/uaa-go-client/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"
)

const (
	DefaultRouterGroupGuid = "bad25cff-9332-48a6-8603-b619858e7992"
	DefaultRouterGroupName = "default-tcp"
	DefaultRouterGroupType = "tcp"
)

var _ = Describe("RouterGroupsHandler", func() {

	var (
		routerGroupHandler *handlers.RouterGroupsHandler
		request            *http.Request
		responseRecorder   *httptest.ResponseRecorder
		fakeClient         *fake_client.FakeClient
		fakeDb             *fake_db.FakeDB
		logger             *lagertest.TestLogger
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test-router-group")
		fakeClient = &fake_client.FakeClient{}
		fakeDb = &fake_db.FakeDB{}
		routerGroupHandler = handlers.NewRouteGroupsHandler(fakeClient, logger, fakeDb)
		responseRecorder = httptest.NewRecorder()

		fakeRouterGroups := []models.RouterGroup{
			{
				Guid:            DefaultRouterGroupGuid,
				Name:            DefaultRouterGroupName,
				Type:            DefaultRouterGroupType,
				ReservablePorts: "1024-65535",
			},
		}
		fakeDb.ReadRouterGroupsReturns(fakeRouterGroups, nil)
	})

	Describe("ListRouterGroups", func() {
		It("responds with 200 OK and returns default router group details", func() {
			var err error
			request, err = http.NewRequest("GET", routing_api.ListRouterGroups, nil)
			Expect(err).NotTo(HaveOccurred())
			routerGroupHandler.ListRouterGroups(responseRecorder, request)
			Expect(responseRecorder.Code).To(Equal(http.StatusOK))
			payload := responseRecorder.Body.String()
			Expect(payload).To(MatchJSON(`[
			{
				  "guid": "bad25cff-9332-48a6-8603-b619858e7992",
					"name": "default-tcp",
					"type": "tcp",
					"reservable_ports": "1024-65535"
			}]`))
		})

		It("checks for routing.router_groups.read scope", func() {
			var err error
			request, err = http.NewRequest("GET", routing_api.ListRouterGroups, nil)
			Expect(err).NotTo(HaveOccurred())
			routerGroupHandler.ListRouterGroups(responseRecorder, request)
			_, permission := fakeClient.DecodeTokenArgsForCall(0)
			Expect(permission).To(ConsistOf(handlers.RouterGroupsReadScope))
		})

		Context("when authorization token is invalid", func() {
			var (
				currentCount int64
			)
			BeforeEach(func() {
				currentCount = metrics.GetTokenErrors()
				fakeClient.DecodeTokenReturns(errors.New("kaboom"))
			})

			It("returns Unauthorized error", func() {
				var err error
				request, err = http.NewRequest("GET", routing_api.ListRouterGroups, nil)
				Expect(err).NotTo(HaveOccurred())
				routerGroupHandler.ListRouterGroups(responseRecorder, request)
				Expect(responseRecorder.Code).To(Equal(http.StatusUnauthorized))
				Expect(metrics.GetTokenErrors()).To(Equal(currentCount + 1))
			})
		})

	})

})
