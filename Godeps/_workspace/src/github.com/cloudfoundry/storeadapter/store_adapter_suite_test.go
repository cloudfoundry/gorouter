package storeadapter_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestStoreAdapter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Store Adapter Suite")
}
