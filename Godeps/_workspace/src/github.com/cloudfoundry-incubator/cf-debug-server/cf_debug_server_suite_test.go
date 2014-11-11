package cf_debug_server_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestCfDebugServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CfDebugServer Suite")
}
