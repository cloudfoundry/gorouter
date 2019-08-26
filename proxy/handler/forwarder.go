package handler

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/uber-go/zap"
)

type Forwarder struct {
	BackendReadTimeout time.Duration
	Logger             logger.Logger
}

// ForwardIO sets up websocket forwarding with a backend
//
// It returns after one of the connections closes.
//
// If the backend response code is not 101 Switching Protocols, then
// ForwardIO will return immediately, allowing the caller to close the connections.
func (f *Forwarder) ForwardIO(clientConn, backendConn io.ReadWriter) int {
	done := make(chan bool, 2)

	copy := func(dst io.Writer, src io.Reader) {
		// don't care about errors here
		_, _ = io.Copy(dst, src)
		done <- true
	}

	headerBytes := &bytes.Buffer{}
	teedReader := io.TeeReader(backendConn, headerBytes)

	resp, err := utils.ReadResponseWithTimeout(bufio.NewReader(teedReader), nil, f.BackendReadTimeout)
	if err != nil {
		switch err.(type) {
		case utils.TimeoutError:
			f.Logger.Error("websocket-forwardio", zap.Error(err))
		}
		return 0
	}

	// we always write the header...
	_, err = io.Copy(clientConn, headerBytes) // don't care about errors
	if err != nil {
		f.Logger.Error("websocket-copy", zap.Error(err))
		return 0
	}

	if !isValidWebsocketResponse(resp) {
		return resp.StatusCode
	}

	// only now do we start copying body data
	go copy(clientConn, backendConn)
	go copy(backendConn, clientConn)

	<-done
	return http.StatusSwitchingProtocols
}

func isValidWebsocketResponse(resp *http.Response) bool {
	ok := resp.StatusCode == http.StatusSwitchingProtocols
	return ok
}
