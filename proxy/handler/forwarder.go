package handler

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
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
		f.Logger.Error("websocket-forwardio", zap.Error(err))
		// we have to write our own HTTP header since we didn't get one from the backend
		_, writeErr := clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		if writeErr != nil {
			f.Logger.Error("websocket-client-write", zap.Error(writeErr))
		}
		return http.StatusBadGateway
	}

	// as long as we got a valid response from the backend,
	// we always write the header...
	_, err = io.Copy(clientConn, headerBytes)
	if err != nil {
		f.Logger.Error("websocket-client-write", zap.Error(err))
		// we got a status code from the backend,
		//
		// we don't know for sure that this got back to the client
		// but there isn't much we can do about that at this point
		//
		// return it so we can log it in access logs
		return resp.StatusCode
	}

	if !isValidWebsocketResponse(resp) {
		errMsg := fmt.Sprintf("backend responded with non-101 status code: %d", resp.StatusCode)
		err = errors.New(errMsg)
		f.Logger.Error("websocket-backend", zap.Error(err))
		return resp.StatusCode
	}

	// only now do we start copying body data
	go copy(clientConn, backendConn)
	go copy(backendConn, clientConn)

	// Note: this blocks until the entire websocket activity completes
	<-done
	return http.StatusSwitchingProtocols
}

func isValidWebsocketResponse(resp *http.Response) bool {
	ok := resp.StatusCode == http.StatusSwitchingProtocols
	return ok
}
