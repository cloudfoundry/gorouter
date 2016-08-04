package test

import (
	"io"
	"net/http"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"github.com/nats-io/nats"
)

func NewGreetApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, tags map[string]string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, tags, "")
	app.AddHandler("/", greetHandler)

	return app
}

func greetHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Hello, world")
}
