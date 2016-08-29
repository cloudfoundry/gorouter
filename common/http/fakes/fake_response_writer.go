package httpfakes

import "net/http"

type FakeResponseWriter struct {
	http.Response
}

func NewFakeResponseWriter() *FakeResponseWriter {
	return &FakeResponseWriter{http.Response{
		Header: make(http.Header),
	}}
}

func (w *FakeResponseWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (w *FakeResponseWriter) Header() http.Header {
	return w.Response.Header
}
func (w *FakeResponseWriter) WriteHeader(status int) {
	w.Response.StatusCode = status
}
