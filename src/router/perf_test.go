package router

import (
	"encoding/base64"
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
	p := NewProxy(s, nil, NewVarz(), NewRegistry())

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
