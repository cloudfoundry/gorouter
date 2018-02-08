package test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/nats-io/go-nats"
	"github.com/onsi/ginkgo"

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
		Expect(r.Header.Get("Connection")).To(Equal("upgrade"))

		conn, _, err := w.(http.Hijacker).Hijack()
		x := test_util.NewHttpConn(conn)

		resp := test_util.NewResponse(http.StatusSwitchingProtocols)
		resp.Header.Set("Upgrade", "websocket")
		resp.Header.Set("Connection", "upgrade")

		time.Sleep(delay)

		x.WriteResponse(resp)
		Expect(err).ToNot(HaveOccurred())

		x.CheckLine("hello from client")
		x.WriteLine("hello from server")
	})

	return app
}

func NewHangingWebSocketApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, routeServiceUrl string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, nil, routeServiceUrl)
	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		defer ginkgo.GinkgoRecover()

		Expect(r.Header.Get("Upgrade")).To(Equal("websocket"))
		Expect(r.Header.Get("Connection")).To(Equal("upgrade"))

		conn, _, err := w.(http.Hijacker).Hijack()
		Expect(err).ToNot(HaveOccurred())
		x := test_util.NewHttpConn(conn)

		resp := test_util.NewResponse(http.StatusNotFound)
		resp.ContentLength = -1
		resp.Header.Set("Upgrade", "websocket")
		resp.Header.Set("Connection", "upgrade")

		fmt.Println("setting body")
		resp.Body = ioutil.NopCloser(io.MultiReader(
			bytes.NewBufferString("\r\nbeginning of the response body goes here\r\n\r\n"),
			bytes.NewBuffer(make([]byte, 10024)), // bigger than the internal buffer of the http stdlib
			bytes.NewBufferString("\r\nmore response here, probably won't be seen by client\r\n"),
			&test_util.HangingReadCloser{}),
		)
		fmt.Println("writing response")
		x.WriteResponse(resp)
		panic("you won't get here in a test")
	})

	return app
}
