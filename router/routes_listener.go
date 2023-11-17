package router

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	common "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
)

type RoutesListener struct {
	Config        *config.Config
	RouteRegistry json.Marshaler

	listener net.Listener
}

func (rl *RoutesListener) ListenAndServe() error {
	hs := http.NewServeMux()
	hs.HandleFunc("/routes", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		enc.Encode(rl.RouteRegistry)
	})

	f := func(user, password string) bool {
		return user == rl.Config.Status.User && password == rl.Config.Status.Pass
	}

	addr := fmt.Sprintf("127.0.0.1:%d", rl.Config.Status.Routes.Port)
	s := &http.Server{
		Addr:         addr,
		Handler:      &common.BasicAuth{Handler: hs, Authenticator: f},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	rl.listener = l

	go func() {
		err = s.Serve(l)
	}()
	return nil
}

func (rl *RoutesListener) Stop() {
	if rl.listener != nil {
		rl.listener.Close()
	}
}
