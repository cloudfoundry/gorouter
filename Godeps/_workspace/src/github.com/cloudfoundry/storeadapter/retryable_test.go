package storeadapter_test

import (
	"errors"
	"time"

	. "github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Retryable", func() {
	var (
		innerStoreAdapter *fakes.FakeStoreAdapter
		retryPolicy       *fakes.FakeRetryPolicy
		sleeper           *fakes.FakeSleeper

		adapter StoreAdapter
	)

	BeforeEach(func() {
		innerStoreAdapter = new(fakes.FakeStoreAdapter)
		retryPolicy = new(fakes.FakeRetryPolicy)
		sleeper = new(fakes.FakeSleeper)

		adapter = NewRetryable(innerStoreAdapter, sleeper, retryPolicy)
	})

	itRetries := func(action func() error, resultIn func(error), attempts func() int, example func()) {
		var errResult error

		JustBeforeEach(func() {
			errResult = action()
		})

		Context("when the store adapter returns a timeout error", func() {
			BeforeEach(func() {
				resultIn(ErrorTimeout)
			})

			Context("as long as the backoff policy returns true", func() {
				BeforeEach(func() {
					durations := make(chan time.Duration, 3)
					durations <- time.Second
					durations <- 2 * time.Second
					durations <- 1000 * time.Second
					close(durations)

					retryPolicy.DelayForStub = func(failedAttempts uint) (time.Duration, bool) {
						Expect(attempts()).To(Equal(int(failedAttempts)))

						select {
						case d, ok := <-durations:
							return d, ok
						}
					}
				})

				It("continuously retries with an increasing attempt count", func() {
					Expect(retryPolicy.DelayForCallCount()).To(Equal(4))
					Expect(sleeper.SleepCallCount()).To(Equal(3))

					Expect(retryPolicy.DelayForArgsForCall(0)).To(Equal(uint(1)))
					Expect(sleeper.SleepArgsForCall(0)).To(Equal(time.Second))

					Expect(retryPolicy.DelayForArgsForCall(1)).To(Equal(uint(2)))
					Expect(sleeper.SleepArgsForCall(1)).To(Equal(2 * time.Second))

					Expect(retryPolicy.DelayForArgsForCall(2)).To(Equal(uint(3)))
					Expect(sleeper.SleepArgsForCall(2)).To(Equal(1000 * time.Second))

					Expect(errResult).To(Equal(ErrorTimeout))
				})
			})
		})

		Context("when the store adapter returns a non-timeout error", func() {
			var adapterErr error

			BeforeEach(func() {
				adapterErr = errors.New("oh no!")
				resultIn(adapterErr)
			})

			It("propagates the error", func() {
				Expect(errResult).To(Equal(adapterErr))
			})
		})

		Context("when the store adapter succeeds", func() {
			BeforeEach(func() {
				resultIn(nil)
			})

			example()

			It("does not error", func() {
				Expect(errResult).NotTo(HaveOccurred())
			})
		})
	}

	Describe("Create", func() {
		createdNode := StoreNode{
			Key:   "created-key",
			Value: []byte("created-value"),
		}

		itRetries(func() error {
			return adapter.Create(createdNode)
		}, func(err error) {
			innerStoreAdapter.CreateReturns(err)
		}, func() int {
			return innerStoreAdapter.CreateCallCount()
		}, func() {
			It("passes the node through", func() {
				Expect(innerStoreAdapter.CreateArgsForCall(0)).To(Equal(createdNode))
			})
		})
	})

	Describe("Update", func() {
		updatedNode := StoreNode{
			Key:   "updated-key",
			Value: []byte("updated-value"),
		}

		itRetries(func() error {
			return adapter.Update(updatedNode)
		}, func(err error) {
			innerStoreAdapter.UpdateReturns(err)
		}, func() int {
			return innerStoreAdapter.UpdateCallCount()
		}, func() {
			It("passes the node through", func() {
				Expect(innerStoreAdapter.UpdateArgsForCall(0)).To(Equal(updatedNode))
			})
		})
	})

	Describe("CompareAndSwap", func() {
		oldNode := StoreNode{
			Key:   "old-key",
			Value: []byte("old-value"),
		}

		newNode := StoreNode{
			Key:   "new-key",
			Value: []byte("new-value"),
		}

		itRetries(func() error {
			return adapter.CompareAndSwap(oldNode, newNode)
		}, func(err error) {
			innerStoreAdapter.CompareAndSwapReturns(err)
		}, func() int {
			return innerStoreAdapter.CompareAndSwapCallCount()
		}, func() {
			It("passes the nodes through", func() {
				oldN, newN := innerStoreAdapter.CompareAndSwapArgsForCall(0)
				Expect(oldN).To(Equal(oldNode))
				Expect(newN).To(Equal(newNode))
			})
		})
	})

	Describe("CompareAndSwapByIndex", func() {
		var comparedIndex uint64 = 123
		swappedNode := StoreNode{
			Key:   "swapped-key",
			Value: []byte("swapped-value"),
		}

		itRetries(func() error {
			return adapter.CompareAndSwapByIndex(comparedIndex, swappedNode)
		}, func(err error) {
			innerStoreAdapter.CompareAndSwapByIndexReturns(err)
		}, func() int {
			return innerStoreAdapter.CompareAndSwapByIndexCallCount()
		}, func() {
			It("passes the node and index through", func() {
				index, node := innerStoreAdapter.CompareAndSwapByIndexArgsForCall(0)
				Expect(index).To(Equal(uint64(comparedIndex)))
				Expect(node).To(Equal(swappedNode))
			})
		})
	})

	Describe("SetMulti", func() {
		nodes := []StoreNode{
			{Key: "key-a", Value: []byte("value-a")},
			{Key: "key-b", Value: []byte("value-b")},
		}

		itRetries(func() error {
			return adapter.SetMulti(nodes)
		}, func(err error) {
			innerStoreAdapter.SetMultiReturns(err)
		}, func() int {
			return innerStoreAdapter.SetMultiCallCount()
		}, func() {
			It("passes the nodes through", func() {
				Expect(innerStoreAdapter.SetMultiArgsForCall(0)).To(Equal(nodes))
			})
		})
	})

	Describe("Get", func() {
		nodeToReturn := StoreNode{
			Key:   "returned-key",
			Value: []byte("returned-value"),
		}

		var gotNode StoreNode

		itRetries(func() error {
			var err error

			gotNode, err = adapter.Get("getting-key")
			return err
		}, func(err error) {
			innerStoreAdapter.GetReturns(nodeToReturn, err)
		}, func() int {
			return innerStoreAdapter.GetCallCount()
		}, func() {
			It("passes the key through", func() {
				Expect(innerStoreAdapter.GetArgsForCall(0)).To(Equal("getting-key"))
			})

			It("returns the node", func() {
				Expect(gotNode).To(Equal(nodeToReturn))
			})
		})
	})

	Describe("ListRecursively", func() {
		nodeToReturn := StoreNode{
			Key:   "returned-key",
			Value: []byte("returned-value"),
		}

		var listedNode StoreNode

		itRetries(func() error {
			var err error

			listedNode, err = adapter.ListRecursively("listing-key")
			return err
		}, func(err error) {
			innerStoreAdapter.ListRecursivelyReturns(nodeToReturn, err)
		}, func() int {
			return innerStoreAdapter.ListRecursivelyCallCount()
		}, func() {
			It("passes the key through", func() {
				Expect(innerStoreAdapter.ListRecursivelyArgsForCall(0)).To(Equal("listing-key"))
			})

			It("returns the node", func() {
				Expect(listedNode).To(Equal(nodeToReturn))
			})
		})
	})

	Describe("Delete", func() {
		keysToDelete := []string{"key1", "key2"}

		itRetries(func() error {
			return adapter.Delete(keysToDelete...)
		}, func(err error) {
			innerStoreAdapter.DeleteReturns(err)
		}, func() int {
			return innerStoreAdapter.DeleteCallCount()
		}, func() {
			It("passes the keys through", func() {
				Expect(innerStoreAdapter.DeleteArgsForCall(0)).To(Equal(keysToDelete))
			})
		})
	})

	Describe("DeleteLeaves", func() {
		keysToDelete := []string{"key1", "key2"}

		itRetries(func() error {
			return adapter.DeleteLeaves(keysToDelete...)
		}, func(err error) {
			innerStoreAdapter.DeleteLeavesReturns(err)
		}, func() int {
			return innerStoreAdapter.DeleteLeavesCallCount()
		}, func() {
			It("passes the keys through", func() {
				Expect(innerStoreAdapter.DeleteLeavesArgsForCall(0)).To(Equal(keysToDelete))
			})
		})
	})

	Describe("CompareAndDelete", func() {
		nodesToCAD := []StoreNode{
			{Key: "key-a", Value: []byte("value-a")},
			{Key: "key-b", Value: []byte("value-b")},
		}

		itRetries(func() error {
			return adapter.CompareAndDelete(nodesToCAD...)
		}, func(err error) {
			innerStoreAdapter.CompareAndDeleteReturns(err)
		}, func() int {
			return innerStoreAdapter.CompareAndDeleteCallCount()
		}, func() {
			It("passes the node through", func() {
				nodes := innerStoreAdapter.CompareAndDeleteArgsForCall(0)
				Expect(nodes).To(Equal(nodesToCAD))
			})
		})
	})

	Describe("CompareAndDeleteByIndex", func() {
		nodesToCAD := []StoreNode{
			{Key: "key-a", Value: []byte("value-a")},
			{Key: "key-b", Value: []byte("value-b")},
		}

		itRetries(func() error {
			return adapter.CompareAndDeleteByIndex(nodesToCAD...)
		}, func(err error) {
			innerStoreAdapter.CompareAndDeleteByIndexReturns(err)
		}, func() int {
			return innerStoreAdapter.CompareAndDeleteByIndexCallCount()
		}, func() {
			It("passes the node and index through", func() {
				nodes := innerStoreAdapter.CompareAndDeleteByIndexArgsForCall(0)
				Expect(nodes).To(Equal(nodesToCAD))
			})
		})
	})

	Describe("UpdateDirTTL", func() {
		dirKey := "dir-key"
		var ttlToSet uint64 = 42

		itRetries(func() error {
			return adapter.UpdateDirTTL(dirKey, ttlToSet)
		}, func(err error) {
			innerStoreAdapter.UpdateDirTTLReturns(err)
		}, func() int {
			return innerStoreAdapter.UpdateDirTTLCallCount()
		}, func() {
			It("passes the keys through", func() {
				dir, ttl := innerStoreAdapter.UpdateDirTTLArgsForCall(0)
				Expect(dir).To(Equal(dirKey))
				Expect(ttl).To(Equal(ttlToSet))
			})
		})
	})
})
