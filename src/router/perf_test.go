package router

import (
	"router/config"
	"strconv"
	"testing"
)

const (
	Host = "1.2.3.4"
	Port = 1234
)

func BenchmarkRegister(b *testing.B) {
	r := NewRegistry(config.DefaultConfig())
	p := NewProxy(r, NewVarz(r))

	for i := 0; i < b.N; i++ {
		str := strconv.Itoa(i)
		rm := &registerMessage{
			Host: "localhost",
			Port: uint16(i),
			Uris: []Uri{Uri("bench.vcap.me." + str)},
		}
		p.Register(rm)
	}
}
