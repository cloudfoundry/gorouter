package routeservice_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRouteService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RouteService Suite")
}
