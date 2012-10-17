package router

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"testing"
)

const (
	Host = "1.2.3.4"
	Port = 1234

	SessionKey = "14fbc303b76bacd1e0a3ab641c11d114"

	Session = "QfahjQKyC6Jxb/JHqa1kZAAAAAAAAAAAAAAAAAAAAAA="
)

func BenchmarkEncryption(b *testing.B) {
	s, _ := NewAESSessionEncoder([]byte(SessionKey), base64.StdEncoding)
	config.SessionKey = SessionKey

	for i := 0; i < b.N; i++ {
		s.encryptStickyCookie(Host, Port)
	}
}

func BenchmarkDecryption(b *testing.B) {
	s, _ := NewAESSessionEncoder([]byte(SessionKey), base64.StdEncoding)
	config.SessionKey = SessionKey

	for i := 0; i < b.N; i++ {
		s.decryptStickyCookie(Session)
	}
}

func BenchmarkRegister(b *testing.B) {
	s, _ := NewAESSessionEncoder([]byte(SessionKey), base64.StdEncoding)
	p := NewProxy(s)
	p.status = NewServerStatus()

	for i := 0; i < b.N; i++ {
		str := strconv.Itoa(i)
		rm := &registerMessage{
			Host: "localhost",
			Port: uint16(i),
			Uris: []string{"bench.vcap.me." + str},
		}
		p.Register(rm)
	}
}

func BenchmarkProxy(b *testing.B) {
	b.StopTimer()

	// Start app
	server := &http.Server{
		Addr: ":40899",
	}
	go server.ListenAndServe()

	// New Proxy
	s, _ := NewAESSessionEncoder([]byte(SessionKey), base64.StdEncoding)
	p := NewProxy(s)
	p.status = NewServerStatus()

	// Register app
	rm := &registerMessage{
		Host: "localhost",
		Port: 40899,
		Uris: []string{"bench.vcap.me"},
		Tags: map[string]string{"component": "cc", "runtime": "ruby"},
	}
	p.Register(rm)

	// Load 10000 registered apps
	for i := 0; i < 10000; i++ {
		str := strconv.Itoa(i)
		rm := &registerMessage{
			Host: "localhost",
			Port: uint16(i),
			Uris: []string{"bench.vcap.me." + str},
		}
		p.Register(rm)
	}

	// New request and response writer
	req, _ := http.NewRequest("GET", "bench.vcap.me", nil)
	req.Host = "bench.vcap.me"
	rw := new(NullResponseWriter)

	b.StartTimer()

	for i := 0; i < b.N; i++ {
		p.ServeHTTP(rw, req)
	}
}

type NullResponseWriter struct {
	header http.Header
}

func (rw *NullResponseWriter) Header() http.Header {
	return rw.header
}

func (rw *NullResponseWriter) Write(b []byte) (int, error) {
	return 0, nil
}

func (rw *NullResponseWriter) WriteHeader(i int) {
	// do nothing
}
