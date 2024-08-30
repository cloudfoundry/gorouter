package route_fetcher_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRouteFetcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RouteFetcher Suite")
}
