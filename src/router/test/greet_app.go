package test

import (
	nats "github.com/cloudfoundry/gonats"
	"io"
	"net/http"
)

func NewGreetApp(urls []string, rPort uint16, natsClient *nats.Client, tags map[string]string) *TestApp {
	app := NewTestApp(urls, rPort, natsClient, tags)
	app.AddHandler("/", greetHandler)

	return app
}

func greetHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Hello, world")
}
