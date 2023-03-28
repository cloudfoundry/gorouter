package config_test

import (
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/lager/v3"
	"code.cloudfoundry.org/lager/v3/lagertest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"
)

var (
	logger lager.Logger
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	test_util.RunSpecWithHoneyCombReporter(t, "Config Suite")
}

var _ = BeforeEach(func() {
	logger = lagertest.NewTestLogger("test")
})
