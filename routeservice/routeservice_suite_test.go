package routeservice_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRouteService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RouteService Suite")
}
