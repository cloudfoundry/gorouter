package main_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os/exec"
	"time"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/testrunner"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
)

var session *Session

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
		session = RoutingApi("-port=65538", "-config=../../example_config/bad_uaa_verification_key.yml", "-ip='127.0.0.1'", "-systemDomain='domain")
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("Port must be in range 0 - 65535"))
	})

	It("exits 1 if no system domain is provided", func() {
		session = RoutingApi("-config=../../example_config/example.yml", "-ip='1.1.1.1'")
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("No system domain provided"))
	})

	It("exits 1 if the uaa_verification_key is not a valid PEM format", func() {
		session = RoutingApi("-config=../../example_config/bad_uaa_verification_key.yml", "-ip='127.0.0.1'", "-systemDomain='domain'", etcdUrl)
		Eventually(session).Should(Exit(1))
		Eventually(session).Should(Say("Public uaa token must be PEM encoded"))
	})

	Context("when initialized correctly and etcd is running", func() {
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
				Expect(routerGroups).To(Equal([]db.RouterGroup{helpers.GetDefaultRouterGroup()}))
			})
		})

		Context("when tcp routes create endpoint is invoked", func() {
			var (
				proc ifrit.Process
				tcpRouteMapping1 db.TcpRouteMapping
				tcpRouteMapping2 db.TcpRouteMapping
			)

			BeforeEach(func() {
				routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
				proc = ifrit.Invoke(routingAPIRunner)
			})

			AfterEach(func() {
				ginkgomon.Interrupt(proc)
			})

			It("allows to create given tcp route mappings", func() {
				client := routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))
				
				tcpRouteMapping1 = db.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000)
				tcpRouteMapping2 = db.NewTcpRouteMapping("router-group-guid-001", 52001, "1.2.3.5", 60001)

				err := client.UpsertTcpRouteMappings([]db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2})
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when tcp routes endpoint is invoked", func() {
			var (
				proc ifrit.Process
				tcpRouteMapping1 db.TcpRouteMapping
				tcpRouteMapping2 db.TcpRouteMapping
				tcpRouteMappings []db.TcpRouteMapping
				client routing_api.Client
			)

			BeforeEach(func() {
				routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
				proc = ifrit.Invoke(routingAPIRunner)
			})

			JustBeforeEach(func(){
				client = routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))
				
				tcpRouteMapping1 = db.NewTcpRouteMapping("router-group-guid-001", 52000, "1.2.3.4", 60000)
				tcpRouteMapping2 = db.NewTcpRouteMapping("router-group-guid-001", 52001, "1.2.3.5", 60001)
				tcpRouteMappings = []db.TcpRouteMapping{tcpRouteMapping1, tcpRouteMapping2}
				err := client.UpsertTcpRouteMappings(tcpRouteMappings)

				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				ginkgomon.Interrupt(proc)
			})

			It("allows to retrieve tcp route mappings", func() {
				tcpRouteMappingsResponse, err := client.TcpRouteMappings()
				Expect(err).NotTo(HaveOccurred())
				Expect(tcpRouteMappingsResponse).NotTo(BeNil())
				Expect(tcpRouteMappingsResponse).To(Equal(tcpRouteMappings))
			})
		})

		It("closes open event streams when the process exits", func() {
			routingAPIRunner := testrunner.New(routingAPIBinPath, routingAPIArgs)
			proc := ifrit.Invoke(routingAPIRunner)
			client := routing_api.NewClient(fmt.Sprintf("http://127.0.0.1:%d", routingAPIPort))

			events, err := client.SubscribeToEvents()
			Expect(err).ToNot(HaveOccurred())

			client.UpsertRoutes([]db.Route{
				db.Route{
					Route:   "some-route",
					Port:    1234,
					IP:      "234.32.43.4",
					TTL:     5,
					LogGuid: "some-guid",
				},
			})

			Eventually(func() string {
				event, _ := events.Next()
				return event.Action
			}).Should(Equal("Upsert"))

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
