package round_tripper

import (
	"bytes"
	"io"
	"net/http"
)

const rewindableContentLengthLimit = 1 << 20 // 1M

// A rewindableBody is used to re-read a http.Request's body on retries
type rewindableBody []byte

// tryPreBufferBody tries to pre-buffer an incoming request's body into a byte array.
// The array can be used to reset the request's body later.
func tryPreBufferBody(req *http.Request) (*rewindableBody, error) {
	if req.Body == nil {
		return nil, nil
	}

	if req.ContentLength == 0 || req.ContentLength > rewindableContentLengthLimit {
		return nil, nil
	}

	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	body := rewindableBody(data)

	return &body, nil
}

// rewindRequest resets a request's body so that it can be read again.
func (body *rewindableBody) rewindRequest(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(*body))
	}
}
