package helpers_test

import (
	"errors"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	"github.com/cloudfoundry-incubator/routing-api/helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("Helpers", func() {
	Describe("RouteRegister", func() {
		var (
			routeRegister *helpers.RouteRegister
			database      *fake_db.FakeDB
			route         db.Route
			logger        *lagertest.TestLogger

			timeChan chan time.Time
			ticker   *time.Ticker
		)

		var process ifrit.Process

		BeforeEach(func() {
			route = db.Route{
				Route:   "i dont care",
				Port:    3000,
				IP:      "i dont care even more",
				TTL:     120,
				LogGuid: "i care a little bit more now",
			}
			database = &fake_db.FakeDB{}
			logger = lagertest.NewTestLogger("event-handler-test")

			timeChan = make(chan time.Time)
			ticker = &time.Ticker{C: timeChan}

			routeRegister = helpers.NewRouteRegister(database, route, ticker, logger)
		})

		AfterEach(func() {
			process.Signal(syscall.SIGTERM)
		})

		JustBeforeEach(func() {
			process = ifrit.Invoke(routeRegister)
		})

		Context("registration", func() {

			Context("with no errors", func() {
				BeforeEach(func() {
					database.SaveRouteStub = func(route db.Route) error {
						return nil
					}

				})

				It("registers the route for a routing api on init", func() {
					Eventually(database.SaveRouteCallCount).Should(Equal(1))
					Eventually(func() db.Route { return database.SaveRouteArgsForCall(0) }).Should(Equal(route))
				})

				It("registers on an interval", func() {
					timeChan <- time.Now()

					Eventually(database.SaveRouteCallCount).Should(Equal(2))
					Eventually(func() db.Route { return database.SaveRouteArgsForCall(1) }).Should(Equal(route))
					Eventually(logger.Logs).Should(HaveLen(0))
				})
			})

			Context("when there are errors", func() {
				BeforeEach(func() {
					database.SaveRouteStub = func(route db.Route) error {
						return errors.New("beep boop, self destruct mode engaged")
					}
				})

				It("only logs the error once for each attempt", func() {

					Consistently(func() int { return len(logger.Logs()) }).Should(BeNumerically("<=", 1))
					Eventually(func() string {
						if len(logger.Logs()) > 0 {
							return logger.Logs()[0].Data["error"].(string)
						} else {
							return ""
						}
					}).Should(ContainSubstring("beep boop, self destruct mode engaged"))
				})
			})
		})

		Context("unregistration", func() {
			It("unregisters the routing api when a SIGTERM is received", func() {
				process.Signal(syscall.SIGTERM)
				Eventually(database.DeleteRouteCallCount).Should(Equal(1))
				Eventually(func() db.Route {
					return database.DeleteRouteArgsForCall(0)
				}).Should(Equal(route))
			})
		})
	})

	Describe("GetDefaultRouterGroup", func() {
		It("returns default router group", func() {
			Expect(helpers.GetDefaultRouterGroup()).To(Equal(db.RouterGroup{
				Name:     "default_tcp",
				Features: []db.Feature{"tcp"},
				Guid:     "bad25cff-9332-48a6-8603-b619858e7992",
			}))
		})
	})
})
