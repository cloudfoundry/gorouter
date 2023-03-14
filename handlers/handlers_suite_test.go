package handlers_test

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"
)

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	test_util.RunSpecWithHoneyCombReporter(t, "Handlers Suite")
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
