package common_test

import (
	. "code.cloudfoundry.org/gorouter/common"
	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	"github.com/nats-io/nats.go"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"
)

type MarshalableValue struct {
	Value map[string]string
}

func (m *MarshalableValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Value)
}

var _ = Describe("Component", func() {
	var (
		component *VcapComponent
		varz      *health.Varz
	)

	BeforeEach(func() {
		port := test_util.NextAvailPort()

		varz = &health.Varz{
			GenericVarz: health.GenericVarz{
				Host:        fmt.Sprintf("127.0.0.1:%d", port),
				Credentials: []string{"username", "password"},
			},
		}
		component = &VcapComponent{
			Varz: varz,
		}
	})

	It("fails to start when the configured port is in use", func() {
		component.Logger = test_util.NewTestZapLogger("test")
		component.Varz.Type = "TestType"

		srv, err := net.Listen("tcp", varz.GenericVarz.Host)
		Expect(err).ToNot(HaveOccurred())
		Expect(srv).ToNot(BeNil())
		err = component.Start()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal(fmt.Sprintf("listen tcp %s: bind: address already in use", varz.GenericVarz.Host)))
		err = srv.Close()
		Expect(err).ToNot(HaveOccurred())
	})

	It("prevents unauthorized access", func() {
		path := "/test"

		component.InfoRoutes = map[string]json.Marshaler{
			path: &MarshalableValue{Value: map[string]string{"key": "value"}},
		}
		serveComponent(component)

		req := buildGetRequest(component, path)
		code, _, _ := doGetRequest(req)
		Expect(code).To(Equal(401))

		req = buildGetRequest(component, path)
		req.SetBasicAuth("username", "incorrect-password")
		code, _, _ = doGetRequest(req)
		Expect(code).To(Equal(401))

		req = buildGetRequest(component, path)
		req.SetBasicAuth("incorrect-username", "password")
		code, _, _ = doGetRequest(req)
		Expect(code).To(Equal(401))
	})

	It("allows multiple info routes", func() {
		path1 := "/test1"
		path2 := "/test2"

		component.InfoRoutes = map[string]json.Marshaler{
			path1: &MarshalableValue{Value: map[string]string{"key": "value1"}},
			path2: &MarshalableValue{Value: map[string]string{"key": "value2"}},
		}
		serveComponent(component)

		//access path1
		req := buildGetRequest(component, path1)
		req.SetBasicAuth("username", "password")

		code, header, body := doGetRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"key":"value1"}` + "\n"))

		//access path2
		req = buildGetRequest(component, path2)
		req.SetBasicAuth("username", "password")

		code, header, body = doGetRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"key":"value2"}` + "\n"))
	})

	It("allows authorized access", func() {
		path := "/test"

		component.InfoRoutes = map[string]json.Marshaler{
			path: &MarshalableValue{Value: map[string]string{"key": "value"}},
		}
		serveComponent(component)

		req := buildGetRequest(component, path)
		req.SetBasicAuth("username", "password")

		code, header, body := doGetRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))
		Expect(body).To(Equal(`{"key":"value"}` + "\n"))
	})

	It("updates the uptime statistic", func() {
		stringMap := make(map[string]interface{})
		path := "/varz"
		component.Varz.Type = "Router"
		startComponent(component)

		time.Sleep(2 * time.Second)
		req := buildGetRequest(component, path)
		req.SetBasicAuth("username", "password")

		code, header, body := doGetRequest(req)
		Expect(code).To(Equal(200))
		Expect(header.Get("Content-Type")).To(Equal("application/json"))

		err := json.Unmarshal([]byte(body), &stringMap)
		Expect(err).NotTo(HaveOccurred())

		duration := stringMap["uptime"].(string)
		Expect(duration).NotTo(Equal(`"uptime":"0d:0h:0m:0s"`))
	})

	It("returns 404 for non existent paths", func() {
		serveComponent(component)

		req := buildGetRequest(component, "/non-existent-path")
		req.SetBasicAuth("username", "password")

		code, _, _ := doGetRequest(req)
		Expect(code).To(Equal(404))
	})

	Describe("Register", func() {
		var mbusClient *nats.Conn
		var natsRunner *test_util.NATSRunner
		var logger logger.Logger

		BeforeEach(func() {
			natsPort := test_util.NextAvailPort()
			natsRunner = test_util.NewNATSRunner(int(natsPort))
			natsRunner.Start()
			mbusClient = natsRunner.MessageBus

			logger = test_util.NewTestZapLogger("test")
		})

		AfterEach(func() {
			natsRunner.Stop()
		})

		It("subscribes to vcap.component.discover", func() {
			done := make(chan struct{})
			members := []string{
				"type",
				"index",
				"host",
				"credentials",
				"start",
				"uuid",
				"uptime",
				"num_cores",
				"mem",
				"cpu",
				"log_counts",
			}

			component.Varz.Type = "TestType"
			component.Logger = logger

			err := component.Start()
			Expect(err).ToNot(HaveOccurred())

			err = component.Register(mbusClient)
			Expect(err).ToNot(HaveOccurred())

			_, err = mbusClient.Subscribe("subject", func(msg *nats.Msg) {
				defer GinkgoRecover()
				data := make(map[string]interface{})
				jsonErr := json.Unmarshal(msg.Data, &data)
				Expect(jsonErr).ToNot(HaveOccurred())

				for _, key := range members {
					_, ok := data[key]
					Expect(ok).To(BeTrue())
				}

				close(done)
			})
			Expect(err).ToNot(HaveOccurred())

			err = mbusClient.PublishRequest("vcap.component.discover", "subject", []byte(""))
			Expect(err).ToNot(HaveOccurred())

			Eventually(done).Should(BeClosed())
		})

		It("publishes to vcap.component.announce on start-up", func() {
			done := make(chan struct{})
			members := []string{
				"type",
				"index",
				"host",
				"credentials",
				"start",
				"uuid",
				"uptime",
				"num_cores",
				"mem",
				"cpu",
				"log_counts",
			}

			component.Varz.Type = "TestType"
			component.Logger = logger

			err := component.Start()
			Expect(err).ToNot(HaveOccurred())

			_, err = mbusClient.Subscribe("vcap.component.announce", func(msg *nats.Msg) {
				defer GinkgoRecover()
				data := make(map[string]interface{})
				jsonErr := json.Unmarshal(msg.Data, &data)
				Expect(jsonErr).ToNot(HaveOccurred())

				for _, key := range members {
					_, ok := data[key]
					Expect(ok).To(BeTrue())
				}

				close(done)
			})
			Expect(err).ToNot(HaveOccurred())

			err = component.Register(mbusClient)
			Expect(err).ToNot(HaveOccurred())

			Eventually(done).Should(BeClosed())
		})

		It("can handle an empty reply in the subject", func() {
			component.Varz.Type = "TestType"
			component.Logger = logger

			err := component.Start()
			Expect(err).ToNot(HaveOccurred())

			err = component.Register(mbusClient)
			Expect(err).ToNot(HaveOccurred())

			err = mbusClient.PublishRequest("vcap.component.discover", "", []byte(""))
			Expect(err).ToNot(HaveOccurred())

			Eventually(logger).Should(gbytes.Say("Received message with empty reply"))

			err = mbusClient.PublishRequest("vcap.component.discover", "reply", []byte("hi"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func startComponent(component *VcapComponent) {
	err := component.Start()
	Expect(err).ToNot(HaveOccurred())

	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", component.Varz.Host, 1*time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	Expect(true).ToNot(BeTrue(), "Could not connect to vcap.Component")
}

func serveComponent(component *VcapComponent) {
	component.ListenAndServe()

	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", component.Varz.Host, 1*time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	Expect(true).ToNot(BeTrue(), "Could not connect to vcap.Component")
}

func buildGetRequest(component *VcapComponent, path string) *http.Request {
	req, err := http.NewRequest("GET", "http://"+component.Varz.Host+path, nil)
	Expect(err).ToNot(HaveOccurred())
	return req
}

func doGetRequest(req *http.Request) (int, http.Header, string) {
	var client http.Client
	var resp *http.Response
	var err error

	resp, err = client.Do(req)
	Expect(err).ToNot(HaveOccurred())
	Expect(resp).ToNot(BeNil())

	body, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	Expect(err).ToNot(HaveOccurred())

	return resp.StatusCode, resp.Header, string(body)
}
