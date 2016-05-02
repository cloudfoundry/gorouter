package main_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os/exec"
	"time"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/test_helpers"
	"github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/testrunner"
	"github.com/cloudfoundry-incubator/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
)

var session *Session

const (
	TOKEN_KEY_ENDPOINT     = "/token_key"
	DefaultRouterGroupName = "default-tcp"
)

var _ = Describe("Main", func() {
	AfterEach(func() {
		if session != nil {
			session.Kill()
		}
	})

	It("exits 1 if no config file is provided", func() {
		session = RoutingApi()
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("No configuration file provided"))
	})

	It("exits 1 if no ip address is provided", func() {
		session = RoutingApi("-config=../../example_config/example.yml")
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("No ip address provided"))
	})

	It("exits 1 if an illegal port number is provided", func() {
		session = RoutingApi("-port=65538", "-config=../../example_config/example.yml", "-ip='127.0.0.1'", "-systemDomain='domain")
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("Port must be in range 0 - 65535"))
	})

	It("exits 1 if no system domain is provided", func() {
		session = RoutingApi("-config=../../example_config/example.yml", "-ip='1.1.1.1'")
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("No system domain provided"))
	})

	It("exits 1 if the uaa_verification_key is not a valid PEM format", func() {
		oauthServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", TOKEN_KEY_ENDPOINT),
				ghttp.RespondWith(http.StatusOK, `{"alg":"rsa", "value": "Invalid PEM key" }`),
			),
		)
		args := routingAPIArgs
		args.DevMode = false
		session = RoutingApi(args.ArgSlice()...)
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("Public uaa token must be PEM encoded"))
	})

	It("exits 1 if the uaa_verification_key cannot be fetched on startup and non dev mode", func() {
		oauthServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", TOKEN_KEY_ENDPOINT),
				ghttp.RespondWith(http.StatusInternalServerError, `{}`),
			),
		)
		args := routingAPIArgs
		args.DevMode = false
		session = RoutingApi(args.ArgSlice()...)
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("Failed to get verification key from UAA"))
	})

	Context("when initialized correctly and etcd is running", func() {
		var (
			routerGroupGuid string
		)

		BeforeEach(func() {
			oauthServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", TOKEN_KEY_ENDPOINT),
					ghttp.RespondWith(http.StatusOK, `{"alg":"rsa", "value": "-----BEGIN PUBLIC KEY-----MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDHFr+KICms+tuT1OXJwhCUmR2dKVy7psa8xzElSyzqx7oJyfJ1JZyOzToj9T5SfTIq396agbHJWVfYphNahvZ/7uMXqHxf+ZH9BL1gk9Y6kCnbM5R60gfwjyW1/dQPjOzn9N394zd2FJoFHwdq9Qs0wBugspULZVNRxq7veq/fzwIDAQAB-----END PUBLIC KEY-----" }`),
				),
			)
		})

		It("unregisters from etcd when the process exits", func() {
			routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
			proc := ifrit.Invoke(routingAPIRunner)

			getRoutes := func() string {
				routesPath := fmt.Sprintf("%s/v2/keys/routes", etcdUrl)
				resp, err := http.Get(routesPath)
				Expect(err).ToNot(HaveOccurred())

				body, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				return string(body)
			}
			Eventually(getRoutes).Should(ContainSubstring("api.example.com/routing"))

			ginkgomon.Interrupt(proc)

			Eventually(getRoutes).ShouldNot(ContainSubstring("api.example.com/routing"))
			Eventually(routingAPIRunner.ExitCode()).Should(Equal(0))
		})

		Context("when router groups endpoint is invoked", func() {
			var proc ifrit.Process

			BeforeEach(func() {
				routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
				proc = ifrit.Invoke(routingAPIRunner)
			})

			AfterEach(func() {
				ginkgomon.Interrupt(proc)
			})

			It("returns router groups", func() {
				client := routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))
				routerGroups, err := client.RouterGroups()
				Expect(err).NotTo(HaveOccurred())
				Expect(len(routerGroups)).To(Equal(1))
				Expect(routerGroups[0].Guid).ToNot(BeNil())
				Expect(routerGroups[0].Name).To(Equal(DefaultRouterGroupName))
				Expect(routerGroups[0].Type).To(Equal(models.RouterGroupType("tcp")))
				Expect(routerGroups[0].ReservablePorts).To(Equal(models.ReservablePorts("1024-65535")))
			})
		})

		getRouterGroupGuid := func() string {
			client := routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))
			routerGroups, err := client.RouterGroups()
			Expect(err).NotTo(HaveOccurred())
			Expect(routerGroups).ToNot(HaveLen(0))
			return routerGroups[0].Guid
		}

		Context("when tcp routes create endpoint is invoked", func() {
			var (
				proc             ifrit.Process
				tcpRouteMapping1 models.TcpRouteMapping
				tcpRouteMapping2 models.TcpRouteMapping
			)

			BeforeEach(func() {
				routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
				proc = ifrit.Invoke(routingAPIRunner)
				routerGroupGuid = getRouterGroupGuid()
			})

			AfterEach(func() {
				ginkgomon.Interrupt(proc)
			})

			It("allows to create given tcp route mappings", func() {
				client := routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))
				var err error
				tcpRouteMapping1 = models.NewTcpRouteMapping(routerGroupGuid, 52000, "1.2.3.4", 60000, 60)
				tcpRouteMapping2 = models.NewTcpRouteMapping(routerGroupGuid, 52001, "1.2.3.5", 60001, 1)

				tcpRouteMappings := []models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2}
				err = client.UpsertTcpRouteMappings(tcpRouteMappings)
				Expect(err).NotTo(HaveOccurred())
				tcpRouteMappingsResponse, err := client.TcpRouteMappings()
				Expect(err).NotTo(HaveOccurred())
				Expect(tcpRouteMappingsResponse).NotTo(BeNil())
				mappings := test_helpers.TcpRouteMappings(tcpRouteMappingsResponse)
				Expect(mappings.ContainsAll(tcpRouteMappings...)).To(BeTrue())

				By("letting route expire")
				Eventually(func() bool {
					tcpRouteMappingsResponse, err := client.TcpRouteMappings()
					Expect(err).NotTo(HaveOccurred())
					mappings := test_helpers.TcpRouteMappings(tcpRouteMappingsResponse)
					return mappings.Contains(tcpRouteMapping2)
				}, 3, 1).Should(BeFalse())
			})

		})

		Context("when tcp routes delete endpoint is invoked", func() {
			var (
				proc             ifrit.Process
				tcpRouteMapping1 models.TcpRouteMapping
				tcpRouteMapping2 models.TcpRouteMapping
				tcpRouteMappings []models.TcpRouteMapping
				client           routing_api.Client
				err              error
			)

			BeforeEach(func() {
				client = routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))
				routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
				proc = ifrit.Invoke(routingAPIRunner)
				routerGroupGuid = getRouterGroupGuid()
			})

			AfterEach(func() {
				ginkgomon.Interrupt(proc)
			})

			JustBeforeEach(func() {
				tcpRouteMapping1 = models.NewTcpRouteMapping(routerGroupGuid, 52000, "1.2.3.4", 60000, 60)
				tcpRouteMapping2 = models.NewTcpRouteMapping(routerGroupGuid, 52001, "1.2.3.5", 60001, 60)
				tcpRouteMappings = []models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2}
				err = client.UpsertTcpRouteMappings(tcpRouteMappings)

				Expect(err).NotTo(HaveOccurred())
			})

			It("allows to delete given tcp route mappings", func() {
				err := client.DeleteTcpRouteMappings(tcpRouteMappings)
				Expect(err).NotTo(HaveOccurred())

				tcpRouteMappingsResponse, err := client.TcpRouteMappings()
				Expect(err).NotTo(HaveOccurred())
				Expect(tcpRouteMappingsResponse).NotTo(BeNil())
				Expect(tcpRouteMappingsResponse).NotTo(ConsistOf(tcpRouteMappings))
			})
		})
		Context("when tcp routes endpoint is invoked", func() {
			var (
				proc             ifrit.Process
				tcpRouteMapping1 models.TcpRouteMapping
				tcpRouteMapping2 models.TcpRouteMapping
				tcpRouteMappings []models.TcpRouteMapping
				client           routing_api.Client
				err              error
			)

			BeforeEach(func() {
				routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
				proc = ifrit.Invoke(routingAPIRunner)
				routerGroupGuid = getRouterGroupGuid()
			})

			JustBeforeEach(func() {
				client = routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))

				tcpRouteMapping1 = models.NewTcpRouteMapping(routerGroupGuid, 52000, "1.2.3.4", 60000, 60)
				tcpRouteMapping2 = models.NewTcpRouteMapping(routerGroupGuid, 52001, "1.2.3.5", 60001, 60)
				tcpRouteMappings = []models.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2}
				err = client.UpsertTcpRouteMappings(tcpRouteMappings)

				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				ginkgomon.Interrupt(proc)
			})

			It("allows to retrieve tcp route mappings", func() {
				tcpRouteMappingsResponse, err := client.TcpRouteMappings()
				Expect(err).NotTo(HaveOccurred())
				Expect(tcpRouteMappingsResponse).NotTo(BeNil())
				Expect(test_helpers.TcpRouteMappings(tcpRouteMappingsResponse).ContainsAll(tcpRouteMappings...)).To(BeTrue())
			})
		})

		It("closes open event streams when the process exits", func() {
			routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
			proc := ifrit.Invoke(routingAPIRunner)
			client := routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))

			events, err := client.SubscribeToEvents()
			Expect(err).ToNot(HaveOccurred())

			client.UpsertRoutes([]models.Route{
				models.Route{
					Route:   "some-route",
					Port:    1234,
					IP:      "234.32.43.4",
					TTL:     1,
					LogGuid: "some-guid",
				},
			})

			Eventually(func() string {
				event, _ := events.Next()
				return event.Action
			}).Should(Equal("Upsert"))

			Eventually(func() string {
				event, _ := events.Next()
				return event.Action
			}, 3, 1).Should(Equal("Delete"))

			ginkgomon.Interrupt(proc)

			Eventually(func() error {
				_, err = events.Next()
				return err
			}).Should(HaveOccurred())

			Eventually(routingAPIRunner.ExitCode(), 2*time.Second).Should(Equal(0))
		})

		It("exits 1 if etcd returns an error as we unregister ourself during a deployment roll", func() {
			routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
			proc := ifrit.Invoke(routingAPIRunner)

			etcdAdapter.Disconnect()
			etcdRunner.Stop()

			ginkgomon.Interrupt(proc)
			Eventually(routingAPIRunner).Should(Exit(1))
		})
	})
})

func RoutingApi(args ...string) *Session {
	session, err := Start(exec.Command(routingAPIBinPath, args...), GinkgoWriter, GinkgoWriter)
	Expect(err).ToNot(HaveOccurred())

	return session
}
