package utils

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"time"
)

type ReadResponseResult struct {
	response *http.Response
	error    error
}

type TimeoutError struct{}

func (t TimeoutError) Error() string {
	return fmt.Sprintf("timeout waiting for http response from backend")
}

// ReadResponseWithTimeout extends http.ReadResponse but it utilizes a timeout
func ReadResponseWithTimeout(r *bufio.Reader, req *http.Request, timeout time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	read := make(chan ReadResponseResult)
	defer close(read)

	go waitForReadResponse(ctx, read, r, req)

	select {
	case s := <-read:
		return s.response, s.error
	case <-ctx.Done():
		return nil, TimeoutError{}
	}
}

func waitForReadResponse(ctx context.Context, c chan<- ReadResponseResult, r *bufio.Reader, req *http.Request) {
	resp, err := http.ReadResponse(r, req)

	select {
	case <-ctx.Done():
	default:
		c <- ReadResponseResult{resp, err}
	}
}
