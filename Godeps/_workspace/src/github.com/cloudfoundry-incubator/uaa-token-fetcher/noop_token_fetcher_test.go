package token_fetcher_test

import (
	. "github.com/cloudfoundry-incubator/uaa-token-fetcher"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("NoopTokenFetcher", func() {

	Context("New", func() {

		var fetcher *NoOpTokenFetcher

		BeforeEach(func() {
			fetcher = NewNoOpTokenFetcher()
		})

		It("returns a no-op token fetcher", func() {
			Expect(fetcher).NotTo(BeNil())
			Expect(fetcher).To(BeAssignableToTypeOf(&NoOpTokenFetcher{}))
		})

		It("returns an empty access token", func() {
			token, err := fetcher.FetchToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token.AccessToken).To(BeEmpty())
		})

	})
})
