package http_server

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/tedsuo/ifrit"
)

type httpServer struct {
	address string
	handler http.Handler

	connectionWaitGroup   *sync.WaitGroup
	inactiveConnections   map[net.Conn]struct{}
	inactiveConnectionsMu *sync.Mutex
	stoppingChan          chan struct{}

	tlsConfig *tls.Config
}

func New(address string, handler http.Handler) ifrit.Runner {
	return &httpServer{
		address: address,
		handler: handler,
	}
}

func NewTLSServer(address string, handler http.Handler, tlsConfig *tls.Config) ifrit.Runner {
	return &httpServer{
		address:   address,
		handler:   handler,
		tlsConfig: tlsConfig,
	}
}

func (s *httpServer) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	s.connectionWaitGroup = new(sync.WaitGroup)
	s.inactiveConnectionsMu = new(sync.Mutex)
	s.inactiveConnections = make(map[net.Conn]struct{})
	s.stoppingChan = make(chan struct{})

	server := http.Server{
		Handler:   s.handler,
		TLSConfig: s.tlsConfig,
		ConnState: func(conn net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				s.connectionWaitGroup.Add(1)
				s.addInactiveConnection(conn)

			case http.StateIdle:
				s.addInactiveConnection(conn)

			case http.StateActive:
				s.removeInactiveConnection(conn)

			case http.StateHijacked, http.StateClosed:
				s.removeInactiveConnection(conn)
				s.connectionWaitGroup.Done()
			}
		},
	}

	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}

	if server.TLSConfig != nil {
		listener = tls.NewListener(tcpKeepAliveListener{listener.(*net.TCPListener)}, server.TLSConfig)
	}

	serverErrChan := make(chan error, 1)
	go func() {
		serverErrChan <- server.Serve(listener)
	}()

	close(ready)

	for {
		select {
		case err = <-serverErrChan:
			return err

		case <-signals:
			close(s.stoppingChan)

			listener.Close()

			s.inactiveConnectionsMu.Lock()
			for c := range s.inactiveConnections {
				c.Close()
			}
			s.inactiveConnectionsMu.Unlock()

			s.connectionWaitGroup.Wait()
			return nil
		}
	}
}

func (s *httpServer) addInactiveConnection(conn net.Conn) {
	select {
	case <-s.stoppingChan:
		conn.Close()
	default:
		s.inactiveConnectionsMu.Lock()
		s.inactiveConnections[conn] = struct{}{}
		s.inactiveConnectionsMu.Unlock()
	}
}

func (s *httpServer) removeInactiveConnection(conn net.Conn) {
	s.inactiveConnectionsMu.Lock()
	delete(s.inactiveConnections, conn)
	s.inactiveConnectionsMu.Unlock()
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}
