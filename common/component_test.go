package common_test

import (
	"strings"

	. "github.com/cloudfoundry/gorouter/common"
	"github.com/cloudfoundry/gorouter/test_util"
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/yagnats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/localip"

	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/cloudfoundry/gunk/natsrunner"
)

type MarshalableValue struct {
	Value map[string]string
}

func (m *MarshalableValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Value)
}

var _ = Describe("Component", func() {
	var component *VcapComponent

	BeforeEach(func() {
		port, err := localip.LocalPort()
		Expect(err).ToNot(HaveOccurred())

		component = &VcapComponent{
			Host:        fmt.Sprintf("127.0.0.1:%d", port),
			Credentials: []string{"username", "password"},
		}
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

	It("returns 404 for non existent paths", func() {
		serveComponent(component)

		req := buildGetRequest(component, "/non-existent-path")
		req.SetBasicAuth("username", "password")

		code, _, _ := doGetRequest(req)
		Expect(code).To(Equal(404))
	})

	Describe("Register", func() {
		var mbusClient yagnats.NATSConn
		var natsRunner *natsrunner.NATSRunner
		var logger *gosteno.Logger
		var sink *gosteno.TestingSink
		BeforeEach(func() {
			natsPort := test_util.NextAvailPort()
			natsRunner = natsrunner.NewNATSRunner(int(natsPort))
			natsRunner.Start()
			mbusClient = natsRunner.MessageBus

			sink = gosteno.NewTestingSink()
			c := &gosteno.Config{
				Sinks: []gosteno.Sink{
					sink,
				},
				Level:     gosteno.LOG_INFO,
				Codec:     gosteno.NewJsonCodec(),
				EnableLOC: true,
			}
			gosteno.Init(c)
			logger = gosteno.NewLogger("test")
		})

		AfterEach(func() {
			natsRunner.Stop()
		})

		It("subscribes to the vcap.component.discover subject", func() {
			component.Varz = &Varz{}
			component.Type = "TestType"
			component.Logger = logger

			err := component.Start()
			Expect(err).ToNot(HaveOccurred())

			err = component.Register(mbusClient)
			Expect(err).ToNot(HaveOccurred())

			err = mbusClient.PublishRequest("vcap.component.discover", "", []byte(""))
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				found := false
				for _, r := range sink.Records() {
					if strings.Contains(r.Message, "Received message with empty reply on subject") {
						found = true
					}
				}
				return found
			}).Should(BeTrue())

			err = mbusClient.PublishRequest("vcap.component.discover", "reply", []byte("hi"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func serveComponent(component *VcapComponent) {
	component.ListenAndServe()

	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", component.Host, 1*time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	Expect(true).ToNot(BeTrue(), "Could not connect to vcap.Component")
}

func buildGetRequest(component *VcapComponent, path string) *http.Request {
	req, err := http.NewRequest("GET", "http://"+component.Host+path, nil)
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
