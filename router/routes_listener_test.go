package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type MarshalableValue struct {
	Value map[string]string
}

func (m *MarshalableValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Value)
}

var _ = Describe("RoutesListener", func() {
	var (
		routesListener *RoutesListener
		registry       *MarshalableValue
		addr           string
		req            *http.Request
		port           uint16
	)

	BeforeEach(func() {
		port = test_util.NextAvailPort()
		addr = "127.0.0.1"
		registry = &MarshalableValue{
			Value: map[string]string{
				"route1": "endpoint1",
			},
		}
		cfg := &config.Config{
			Status: config.StatusConfig{
				User: "test-user",
				Pass: "test-pass",
				Routes: config.StatusRoutesConfig{
					Port: port,
				},
			},
		}

		routesListener = &RoutesListener{
			Config:        cfg,
			RouteRegistry: registry,
		}
		err := routesListener.ListenAndServe()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		routesListener.Stop()
	})

	JustBeforeEach(func() {
		var err error
		req, err = http.NewRequest("GET", fmt.Sprintf("http://%s:%d/routes", addr, port), nil)
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns the route list", func() {
		req.SetBasicAuth("test-user", "test-pass")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())

		Expect(resp.StatusCode).To(Equal(200))
		Expect(resp.Header.Get("Content-Type")).To(Equal("application/json"))

		body, err := io.ReadAll(resp.Body)
		defer resp.Body.Close()
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(Equal(`{"route1":"endpoint1"}` + "\n"))
	})
	It("stops listening", func() {
		routesListener.Stop()
		resp, err := http.DefaultClient.Do(req)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("dial tcp 127.0.0.1:%d: connect: connection refused", port))))
		Expect(resp).To(BeNil())
	})

	Context("when connecting to non-localhost IP", func() {
		BeforeEach(func() {
			conn, err := net.Dial("udp", "8.8.8.8:80")
			Expect(err).ToNot(HaveOccurred())
			defer conn.Close()
			addr = conn.LocalAddr().(*net.UDPAddr).IP.String()
		})
		It("doesn't respond", func() {
			resp, err := http.DefaultClient.Do(req)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("dial tcp %s:%d: connect: connection refused", addr, port))))
			Expect(resp).To(BeNil())
		})
	})
	Context("when no creds are provided", func() {
		It("returns a 401", func() {
			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())

			Expect(resp.StatusCode).To(Equal(401))

			body, err := io.ReadAll(resp.Body)
			defer resp.Body.Close()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(body)).To(Equal("401 Unauthorized\n"))
		})
	})
	Context("when invalid creds are provided", func() {
		It("retuns a 401", func() {
			req.SetBasicAuth("bad-user", "bad-pass")
			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())

			Expect(resp.StatusCode).To(Equal(401))

			body, err := io.ReadAll(resp.Body)
			defer resp.Body.Close()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(body)).To(Equal("401 Unauthorized\n"))
		})
	})
})
