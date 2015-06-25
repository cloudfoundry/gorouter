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
			Expect(node).To(Equal(gorillaBaby))
			Expect(found).To(BeTrue())
		})

		Context("when there is no child that matches", func() {
			It("should return zero value and false", func() {
				node, found := cage.Lookup("babe E.")
				Expect(node).To(BeZero())
				Expect(found).To(BeFalse())
			})
		})
	})

	Describe("KeyComponents", func() {
		It("returns the path segments of the key", func() {
			Expect(gorillaBaby.KeyComponents()).To(Equal([]string{
				"zoo", "apes", "gorillas", "baby",
			}))

		})
	})
})
