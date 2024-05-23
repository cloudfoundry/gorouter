package test

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"github.com/nats-io/nats.go"
)

func NewGreetApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, tags map[string]string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, tags, "")
	app.AddHandler("/", greetHandler)
	app.AddHandler("/forwardedprotoheader", headerHandler)
	app.AddHandler("/continue", continueHandler)

	return app
}

func headerHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, fmt.Sprintf("%+v", r.Header.Get("X-Forwarded-Proto")))
}
func greetHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, fmt.Sprintf("Hello, %s", r.RemoteAddr))
}
func continueHandler(w http.ResponseWriter, r *http.Request) {
	randomInt := rand.Intn(100)
	fmt.Printf("got request %d\n", randomInt)

	if r.Header.Get("Expect") == "100-Continue" {
	}

	if r.Method != http.MethodGet {
		fmt.Printf("not allowed %d\n", randomInt)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("got err: %s %d\n", err.Error(), randomInt)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	fmt.Printf("got body: %s %d\n", string(body), randomInt)
	w.WriteHeader(http.StatusOK)
}
