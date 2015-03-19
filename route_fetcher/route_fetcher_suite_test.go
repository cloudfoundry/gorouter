package route_fetcher_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRouteFetcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RouteFetcher Suite")
}
