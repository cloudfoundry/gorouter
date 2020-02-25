package test

import (
	"fmt"
	"io"
	"net/http"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"github.com/nats-io/nats.go"
)

func NewStickyApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, tags map[string]string, stickyCookieName string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, tags, "")
	app.AddHandler("/sticky", stickyHandler(app.Port(), stickyCookieName))

	return app
}

func stickyHandler(port uint16, stickyCookieName string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie := &http.Cookie{
			Name:  stickyCookieName,
			Value: "xxx",
		}
		http.SetCookie(w, cookie)
		io.WriteString(w, fmt.Sprintf("%d", port))
	}
}
