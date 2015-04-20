package common_test

import (
	. "github.com/cloudfoundry/gorouter/common"
	"github.com/cloudfoundry/yagnats/fakeyagnats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/localip"

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
	var component *VcapComponent

	BeforeEach(func() {
		port, err := localip.LocalPort()
		Ω(err).ShouldNot(HaveOccurred())

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
		Ω(code).Should(Equal(401))

		req = buildGetRequest(component, path)
		req.SetBasicAuth("username", "incorrect-password")
		code, _, _ = doGetRequest(req)
		Ω(code).Should(Equal(401))

		req = buildGetRequest(component, path)
		req.SetBasicAuth("incorrect-username", "password")
		code, _, _ = doGetRequest(req)
		Ω(code).Should(Equal(401))
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
		Ω(code).Should(Equal(200))
		Ω(header.Get("Content-Type")).Should(Equal("application/json"))
		Ω(body).Should(Equal(`{"key":"value1"}` + "\n"))

		//access path2
		req = buildGetRequest(component, path2)
		req.SetBasicAuth("username", "password")

		code, header, body = doGetRequest(req)
		Ω(code).Should(Equal(200))
		Ω(header.Get("Content-Type")).Should(Equal("application/json"))
		Ω(body).Should(Equal(`{"key":"value2"}` + "\n"))
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
		Ω(code).Should(Equal(200))
		Ω(header.Get("Content-Type")).Should(Equal("application/json"))
		Ω(body).Should(Equal(`{"key":"value"}` + "\n"))
	})

	It("returns 404 for non existent paths", func() {
		serveComponent(component)

		req := buildGetRequest(component, "/non-existent-path")
		req.SetBasicAuth("username", "password")

		code, _, _ := doGetRequest(req)
		Ω(code).Should(Equal(404))
	})

	Context("#Register", func() {
		var fakeNats *fakeyagnats.FakeNATSConn
		BeforeEach(func() {
			fakeNats = fakeyagnats.Connect()
		})

		Context("When the JobName is set", func() {
			BeforeEach(func() {
				component = &VcapComponent{
					Host:        fmt.Sprintf("127.0.0.1:%d", 12345),
					Credentials: []string{"username", "password"},
					JobName:     "router_z1",
				}
			})

			It("includes the job_name property in the announce message", func() {
				component.Register(fakeNats)
				Ω(len(fakeNats.PublishedMessages("vcap.component.announce"))).To(Equal(1))
				for _, msg := range fakeNats.PublishedMessages("vcap.component.announce") {
					Ω(msg.Data).To(MatchRegexp("^{.*\"job_name\":\"router_z1\".*}$"))
				}
			})
		})

		Context("When the JobName is not set", func() {
			BeforeEach(func() {
				component = &VcapComponent{
					Host:        fmt.Sprintf("127.0.0.1:%d", 12345),
					Credentials: []string{"username", "password"},
				}
			})

			It("includes the job_name property in the announce message", func() {
				component.Register(fakeNats)
				Ω(len(fakeNats.PublishedMessages("vcap.component.announce"))).To(Equal(1))
				for _, msg := range fakeNats.PublishedMessages("vcap.component.announce") {
					Ω(msg.Data).ShouldNot(MatchRegexp("^{.*\"job_name\":\"router_z1\".*}$"))
				}
			})
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

	Ω(true).ShouldNot(BeTrue(), "Could not connect to vcap.Component")
}

func buildGetRequest(component *VcapComponent, path string) *http.Request {
	req, err := http.NewRequest("GET", "http://"+component.Host+path, nil)
	Ω(err).ShouldNot(HaveOccurred())
	return req
}

func doGetRequest(req *http.Request) (int, http.Header, string) {
	var client http.Client
	var resp *http.Response
	var err error

	resp, err = client.Do(req)
	Ω(err).ShouldNot(HaveOccurred())
	Ω(resp).ShouldNot(BeNil())

	body, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	Ω(err).ShouldNot(HaveOccurred())

	return resp.StatusCode, resp.Header, string(body)
}
