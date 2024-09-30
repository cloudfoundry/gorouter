package accesslog_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAccessLog(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AccessLog Suite")
}
