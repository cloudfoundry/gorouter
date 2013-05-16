package test

import (
	"io"
	"net/http"
  mbus "github.com/cloudfoundry/go_cfmessagebus"
)

func NewGreetApp(urls []string, rPort uint16, mbusClient mbus.CFMessageBus, tags map[string]string) *TestApp {
	app := NewTestApp(urls, rPort, mbusClient, tags)
	app.AddHandler("/", greetHandler)

	return app
}

func greetHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Hello, world")
}
