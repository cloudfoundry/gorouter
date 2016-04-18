package test

import (
	"io"
	"net/http"
	"time"

	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/test/common"
	"github.com/nats-io/nats"
)

func NewSlowApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, delay time.Duration) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, nil, "")

	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		io.WriteString(w, "Hello, world")
	})

	app.AddHandler("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(delay)
		io.WriteString(w, "Hello, world")
	})

	return app
}
