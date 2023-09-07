package route_fetcher_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRouteFetcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RouteFetcher Suite")
}
