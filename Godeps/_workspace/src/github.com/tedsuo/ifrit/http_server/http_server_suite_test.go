package http_server_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestHttpServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HttpServer Suite")
}
