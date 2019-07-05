package config_test

import (
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
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
