package router_test

import (
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/test/common"

	"net"
	"time"
)

func appRegistered(registry *registry.RouteRegistry, app *common.TestApp) bool {
	for _, url := range app.Urls() {
		pool := registry.Lookup(url)
		if pool == nil {
			return false
		}
	}

	return true
}

func appUnregistered(registry *registry.RouteRegistry, app *common.TestApp) bool {
	for _, url := range app.Urls() {
		pool := registry.Lookup(url)
		if pool != nil {
			return false
		}
	}

	return true
}

func timeoutDialler() func(net, addr string) (c net.Conn, err error) {
	return func(netw, addr string) (net.Conn, error) {
		c, err := net.DialTimeout(netw, addr, 10*time.Second)
		c.SetDeadline(time.Now().Add(2 * time.Second))
		return c, err
	}
}
