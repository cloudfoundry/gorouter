package accesslog_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"
)

func TestAccessLog(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AccessLog Suite")
}
