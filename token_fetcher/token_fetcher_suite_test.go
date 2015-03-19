package token_fetcher_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestTokenFetcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TokenFetcher Suite")
}
