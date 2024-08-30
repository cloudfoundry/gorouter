package handlers_test

import (
	"net/http"
	"testing"

	"code.cloudfoundry.org/gorouter/handlers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Handlers Suite")
}

type PrevHandler struct{}

func (h *PrevHandler) ServeHTTP(w http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
	next(w, req)
}

type PrevHandlerWithTrace struct{}

func (h *PrevHandlerWithTrace) ServeHTTP(w http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
	reqInfo, err := handlers.ContextRequestInfo(req)
	if err == nil {
		reqInfo.TraceInfo = handlers.TraceInfo{
			TraceID: "1111",
			SpanID:  "2222",
		}
	}

	next(w, req)
}
