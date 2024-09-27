package test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
)

func NewWebSocketApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, delay time.Duration, routeServiceUrl string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, nil, routeServiceUrl)
	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		defer ginkgo.GinkgoRecover()

		Expect(r.Header.Get("Upgrade")).To(Equal("websocket"))
		Expect(strings.ToLower(r.Header.Get("Connection"))).To(Equal("upgrade"))

		conn, _, err := w.(http.Hijacker).Hijack()
		Expect(err).ToNot(HaveOccurred())
		x := test_util.NewHttpConn(conn)

		resp := test_util.NewResponse(http.StatusSwitchingProtocols)
		resp.Header.Set("Upgrade", "websocket")
		resp.Header.Set("Connection", "upgrade")

		time.Sleep(delay)

		x.WriteResponse(resp)

		x.CheckLine("hello from client")
		// #nosec G104 - ignore errors when writing HTTP responses so we don't spam our logs during a DoS
		x.WriteLine("hello from server")
	})

	return app
}

func NewFailingWebSocketApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, delay time.Duration, routeServiceUrl string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, nil, routeServiceUrl)
	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		defer ginkgo.GinkgoRecover()

		Expect(r.Header.Get("Upgrade")).To(Equal("websocket"))
		Expect(strings.ToLower(r.Header.Get("Connection"))).To(Equal("upgrade"))

		conn, _, err := w.(http.Hijacker).Hijack()
		Expect(err).ToNot(HaveOccurred())
		x := test_util.NewHttpConn(conn)
		err = x.Close()
		Expect(err).ToNot(HaveOccurred())

	})

	return app
}

func NewNotUpgradingWebSocketApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, routeServiceUrl string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, nil, routeServiceUrl)
	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		defer ginkgo.GinkgoRecover()

		Expect(r.Header.Get("Upgrade")).To(Equal("websocket"))
		Expect(strings.ToLower(r.Header.Get("Connection"))).To(Equal("upgrade"))

		conn, _, err := w.(http.Hijacker).Hijack()
		Expect(err).ToNot(HaveOccurred())
		x := test_util.NewHttpConn(conn)

		resp := test_util.NewResponse(http.StatusNotFound)
		resp.ContentLength = -1
		resp.Header.Set("Upgrade", "websocket")
		resp.Header.Set("Connection", "upgrade")

		resp.Body = io.NopCloser(io.MultiReader(
			bytes.NewBufferString("\r\nbeginning of the response body goes here\r\n\r\n"),
			bytes.NewBuffer(make([]byte, 10024)), // bigger than the internal buffer of the http stdlib
			bytes.NewBufferString("\r\nmore response here, probably won't be seen by client\r\n"),
		),
		)
		x.WriteResponse(resp)
	})

	return app
}
