package storeadapter_test

import (
	. "github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Storenode", func() {
	var cage StoreNode
	var gorillaBaby StoreNode

	BeforeEach(func() {
		gorillaBaby = StoreNode{
			Key:   "/zoo/apes/gorillas/baby",
			Value: []byte("qtπ"),
		}

		cage = StoreNode{
			Key: "/zoo/apes/gorillas",
			Dir: true,
			ChildNodes: []StoreNode{
				gorillaBaby,
			},
		}
	})

	Describe("Lookup", func() {
		It("should do a depth=1 search for children with childKey", func() {
			node, found := cage.Lookup("baby")
			Ω(node).Should(Equal(gorillaBaby))
			Ω(found).Should(BeTrue())
		})

		Context("when there is no child that matches", func() {
			It("should return zero value and false", func() {
				node, found := cage.Lookup("babe E.")
				Ω(node).Should(BeZero())
				Ω(found).Should(BeFalse())
			})
		})
	})

	Describe("KeyComponents", func() {
		It("returns the path segments of the key", func() {
			Ω(gorillaBaby.KeyComponents()).Should(Equal([]string{
				"zoo", "apes", "gorillas", "baby",
			}))
		})
	})
})
