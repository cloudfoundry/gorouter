package cf_debug_server

import (
	"flag"
	"net"
	"net/http"
	"net/http/pprof"
)

var debugAddr = flag.String(
	"debugAddr",
	"",
	"host:port for serving pprof debugging info",
)

func Run() {
	if *debugAddr == "" {
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))

	listener, err := net.Listen("tcp", *debugAddr)
	if err != nil {
		panic(err)
	}

	go http.Serve(listener, mux)
}

func Addr() string {
	return *debugAddr
}

func SetAddr(addr string) {
	debugAddr = &addr
}
