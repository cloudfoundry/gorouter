package routing_api_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRoutingApi(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RoutingApi Suite")
}
