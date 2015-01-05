package cf_debug_server

import (
	"flag"
	"net/http"
	"net/http/pprof"

	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/http_server"
)

const (
	DebugFlag = "debugAddr"
)

func AddFlags(flags *flag.FlagSet) {
	flags.String(
		DebugFlag,
		"",
		"host:port for serving pprof debugging info",
	)
}

func DebugAddress(flags *flag.FlagSet) string {
	dbgFlag := flags.Lookup(DebugFlag)
	if dbgFlag == nil {
		return ""
	}

	return dbgFlag.Value.String()
}

func Runner(address string) ifrit.Runner {
	return http_server.New(address, Handler())
}

func Run(address string) error {
	p := ifrit.Invoke(Runner(address))
	select {
	case <-p.Ready():
	case err := <-p.Wait():
		return err
	}
	return nil
}

func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))

	return mux
}
