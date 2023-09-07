package routeservice_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRouteService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RouteService Suite")
}
