package test

import (
	"io"
	"net/http"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"github.com/nats-io/nats"
)

func NewRouteServiceApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, routeService string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, nil, routeService)
	app.AddHandler("/", rsGreetHandler)

	return app
}

func rsGreetHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Hello, world, through a route service")
}
