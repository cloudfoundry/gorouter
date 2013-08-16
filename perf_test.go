package router

import (
	"github.com/cloudfoundry/go_cfmessagebus/mock_cfmessagebus"
	"strconv"
	"testing"
)

const (
	Host = "1.2.3.4"
	Port = 1234
)

func BenchmarkRegister(b *testing.B) {
	c := DefaultConfig()
	mbus := mock_cfmessagebus.NewMockMessageBus()
	r := NewRegistry(c, mbus)
	p := NewProxy(c, r, NewVarz(r))

	for i := 0; i < b.N; i++ {
		str := strconv.Itoa(i)
		p.Register(&RouteEndpoint{
			Host: "localhost",
			Port: uint16(i),
			Uris: []Uri{Uri("bench.vcap.me." + str)},
		})
	}
}
