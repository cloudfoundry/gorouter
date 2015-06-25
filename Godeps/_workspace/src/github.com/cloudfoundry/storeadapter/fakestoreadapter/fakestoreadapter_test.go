package fakestoreadapter_test

import (
	"errors"

	"github.com/cloudfoundry/storeadapter"
	. "github.com/cloudfoundry/storeadapter/fakestoreadapter"
	. "github.com/cloudfoundry/storeadapter/storenodematchers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fakestoreadapter", func() {
	var adapter *FakeStoreAdapter
	var breakfastNode, lunchNode, firstCourseDinnerNode, secondCourseDinnerNode, randomNode storeadapter.StoreNode

	BeforeEach(func() {
		adapter = New()
		breakfastNode = storeadapter.StoreNode{
			Key:   "/menu/breakfast",
			Value: []byte("waffle"),
		}
		lunchNode = storeadapter.StoreNode{
			Key:   "/menu/lunch",
			Value: []byte("burger"),
		}
		firstCourseDinnerNode = storeadapter.StoreNode{
			Key:   "/menu/dinner/first",
			Value: []byte("caesar salad"),
		}
		secondCourseDinnerNode = storeadapter.StoreNode{
			Key:   "/menu/dinner/second",
			Value: []byte("steak"),
		}
		randomNode = storeadapter.StoreNode{
			Key:   "/random",
			Value: []byte("17"),
		}

		err := adapter.SetMulti([]storeadapter.StoreNode{
			breakfastNode,
			lunchNode,
			firstCourseDinnerNode,
			secondCourseDinnerNode,
			randomNode,
		})
		Expect(err).NotTo(HaveOccurred())

		adapter.SetErrInjector = NewFakeStoreAdapterErrorInjector("dom$", errors.New("injected set error"))
		adapter.GetErrInjector = NewFakeStoreAdapterErrorInjector("dom$", errors.New("injected get error"))
		adapter.ListErrInjector = NewFakeStoreAdapterErrorInjector("dom$", errors.New("injected list error"))
		adapter.DeleteErrInjector = NewFakeStoreAdapterErrorInjector("dom$", errors.New("injected delete error"))
		adapter.CreateErrInjector = NewFakeStoreAdapterErrorInjector("dom$", errors.New("injected create error"))
	})

	It("should satisfy the interface", func() {
		var adapterInterface storeadapter.StoreAdapter
		adapterInterface = adapter

		Expect(adapterInterface)
	})

	Describe("Creating", func() {
		Context("when creating an existing key", func() {
			It("should error", func() {
				err := adapter.Create(firstCourseDinnerNode)
				Expect(err).To(Equal(storeadapter.ErrorKeyExists))
			})
		})

		Context("when creating a new key", func() {
			It("should", func() {
				thirdCourseDinnerNode := storeadapter.StoreNode{
					Key:   "/menu/dinner/third",
					Value: []byte("mashed potaters"),
				}

				err := adapter.Create(thirdCourseDinnerNode)
				Expect(err).NotTo(HaveOccurred())

				value, err := adapter.Get("/menu/dinner/third")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal(thirdCourseDinnerNode))
			})
		})

		Context("when the key matches the error injector", func() {
			It("should return the injected error", func() {
				thirdCourseDinnerNode := storeadapter.StoreNode{
					Key:   "/menu/dinner/random",
					Value: []byte("mashed potaters"),
				}

				err := adapter.Create(thirdCourseDinnerNode)
				Expect(err).To(Equal(errors.New("injected create error")))
			})
		})
	})

	Describe("Setting", func() {
		Context("when setting to a directory", func() {
			It("should error", func() {
				badMenu := storeadapter.StoreNode{
					Key:   "/menu",
					Value: []byte("oops"),
				}
				err := adapter.SetMulti([]storeadapter.StoreNode{badMenu})
				Expect(err).To(Equal(storeadapter.ErrorNodeIsDirectory))

				value, err := adapter.Get("/menu/breakfast")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal(breakfastNode))
			})
		})

		Context("when implicitly turning a node into a directory", func() {
			It("should error", func() {
				badBreakfast := storeadapter.StoreNode{
					Key:   "/menu/breakfast/elevensies",
					Value: []byte("oops"),
				}
				err := adapter.SetMulti([]storeadapter.StoreNode{badBreakfast})
				Expect(err).To(Equal(storeadapter.ErrorNodeIsNotDirectory))

				value, err := adapter.Get("/menu/breakfast")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal(breakfastNode))
			})
		})

		Context("when overwriting a key", func() {
			It("should overwrite the key", func() {
				discerningBreakfastNode := storeadapter.StoreNode{
					Key:   "/menu/breakfast",
					Value: []byte("crepes"),
				}
				err := adapter.SetMulti([]storeadapter.StoreNode{discerningBreakfastNode})
				Expect(err).NotTo(HaveOccurred())

				value, err := adapter.Get("/menu/breakfast")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal(discerningBreakfastNode))
			})
		})

		Context("when the key matches the error injector", func() {
			It("should return the injected error", func() {
				lessRandomNode := storeadapter.StoreNode{
					Key:   "/random",
					Value: []byte("0"),
				}

				err := adapter.SetMulti([]storeadapter.StoreNode{lessRandomNode})
				Expect(err).To(Equal(errors.New("injected set error")))

				adapter.GetErrInjector = nil
				value, err := adapter.Get("/random")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal(randomNode))
			})
		})
	})

	Describe("Getting", func() {
		Context("when the key is present", func() {
			It("should return the node", func() {
				value, err := adapter.Get("/menu/breakfast")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal(breakfastNode))
			})
		})

		Context("when the key is missing", func() {
			It("should return the key not found error", func() {
				value, err := adapter.Get("/not/a/key")
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))
				Expect(value).To(BeZero())
			})
		})

		Context("when the key is a directory", func() {
			It("should return the key not found error", func() {
				value, err := adapter.Get("/menu")
				Expect(err).To(Equal(storeadapter.ErrorNodeIsDirectory))
				Expect(value).To(BeZero())
			})
		})

		Context("when the key matches the error injector", func() {
			It("should return the injected error", func() {
				value, err := adapter.Get("/random")
				Expect(err).To(Equal(errors.New("injected get error")))
				Expect(value).To(BeZero())
			})
		})
	})

	Describe("Listing", func() {
		Context("when listing the root directory", func() {
			It("should return the tree of nodes", func() {
				value, err := adapter.ListRecursively("/")
				Expect(err).NotTo(HaveOccurred())
				Expect(value.Key).To(Equal("/"))
				Expect(value.Dir).To(BeTrue())
				Expect(value.ChildNodes).To(HaveLen(2))
				Expect(value.ChildNodes).To(ContainElement(randomNode))

				var menuNode storeadapter.StoreNode
				for _, node := range value.ChildNodes {
					if node.Key == "/menu" {
						menuNode = node
					}
				}
				Expect(menuNode.Key).To(Equal("/menu"))
				Expect(menuNode.Dir).To(BeTrue())
				Expect(menuNode.ChildNodes).To(HaveLen(3))
				Expect(menuNode.ChildNodes).To(ContainElement(breakfastNode))
				Expect(menuNode.ChildNodes).To(ContainElement(lunchNode))

				var dinnerNode storeadapter.StoreNode
				for _, node := range menuNode.ChildNodes {
					if node.Key == "/menu/dinner" {
						dinnerNode = node
					}
				}
				Expect(dinnerNode.Key).To(Equal("/menu/dinner"))
				Expect(dinnerNode.Dir).To(BeTrue())
				Expect(dinnerNode.ChildNodes).To(HaveLen(2))
				Expect(dinnerNode.ChildNodes).To(ContainElement(firstCourseDinnerNode))
				Expect(dinnerNode.ChildNodes).To(ContainElement(secondCourseDinnerNode))
			})
		})

		Context("when listing a subdirectory", func() {
			It("should return the tree of nodes", func() {
				menuNode, err := adapter.ListRecursively("/menu")
				Expect(err).NotTo(HaveOccurred())
				Expect(menuNode.Key).To(Equal("/menu"))
				Expect(menuNode.Dir).To(BeTrue())
				Expect(menuNode.ChildNodes).To(HaveLen(3))
				Expect(menuNode.ChildNodes).To(ContainElement(breakfastNode))
				Expect(menuNode.ChildNodes).To(ContainElement(lunchNode))

				var dinnerNode storeadapter.StoreNode
				for _, node := range menuNode.ChildNodes {
					if node.Key == "/menu/dinner" {
						dinnerNode = node
					}
				}
				Expect(dinnerNode.Key).To(Equal("/menu/dinner"))
				Expect(dinnerNode.Dir).To(BeTrue())
				Expect(dinnerNode.ChildNodes).To(HaveLen(2))
				Expect(dinnerNode.ChildNodes).To(ContainElement(firstCourseDinnerNode))
				Expect(dinnerNode.ChildNodes).To(ContainElement(secondCourseDinnerNode))
			})
		})

		Context("when listing a nonexistent key", func() {
			It("should return the key not found error", func() {
				value, err := adapter.ListRecursively("/not-a-key")
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))
				Expect(value).To(BeZero())
			})
		})

		Context("when listing an entry", func() {
			It("should return the key is not a directory error", func() {
				value, err := adapter.ListRecursively("/menu/breakfast")
				Expect(err).To(Equal(storeadapter.ErrorNodeIsNotDirectory))
				Expect(value).To(BeZero())
			})
		})

		Context("when the key matches the error injector", func() {
			It("should return the injected error", func() {
				adapter.ListErrInjector = NewFakeStoreAdapterErrorInjector("menu", errors.New("injected list error"))
				value, err := adapter.ListRecursively("/menu")
				Expect(err).To(Equal(errors.New("injected list error")))
				Expect(value).To(BeZero())
			})
		})
	})

	Describe("Deleting", func() {
		Context("when the key is present", func() {
			It("should delete the node", func() {
				err := adapter.Delete("/menu/breakfast", "/menu/lunch")
				Expect(err).NotTo(HaveOccurred())

				_, err = adapter.Get("/menu/breakfast")
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))

				_, err = adapter.Get("/menu/lunch")
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when the key is missing", func() {
			It("should return the key not found error", func() {
				err := adapter.Delete("/not/a/key")
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when the key is a directory", func() {
			It("should kaboom the directory", func() {
				err := adapter.Delete("/menu")
				Expect(err).NotTo(HaveOccurred())

				_, err = adapter.Get("/menu")
				_, err = adapter.Get("/menu")
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when the key matches the error injector", func() {
			It("should return the injected error", func() {
				err := adapter.Delete("/random")
				Expect(err).To(Equal(errors.New("injected delete error")))

				value, err := adapter.Get("/menu/breakfast")
				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(Equal(breakfastNode))
			})
		})
	})

	Describe("Compare-and-Deleting", func() {
		var (
			nodeFoo storeadapter.StoreNode
			nodeBar storeadapter.StoreNode
		)

		BeforeEach(func() {
			nodeFoo = storeadapter.StoreNode{
				Key:   "/foo",
				Value: []byte("foo"),
				TTL:   1,
			}

			nodeBar = storeadapter.StoreNode{
				Key:   "/foo",
				Value: []byte("bar"),
				TTL:   2,
			}
		})

		Context("when passed multiple keys", func() {
			It("Panics", func() {
				Expect(func() { adapter.CompareAndDelete(nodeFoo, nodeBar) }).To(Panic())
			})
		})

		Context("when the key is missing", func() {
			It("returns a KeyNotFound error", func() {

				err := adapter.CompareAndDelete(nodeFoo)
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when the Value of the existing node is different", func() {
			BeforeEach(func() {
				adapter.Create(nodeFoo)
			})

			It("does NOT delete the existing node and returns a KeyComparisonFailed error", func() {
				err := adapter.CompareAndDelete(nodeBar)
				Expect(err).To(Equal(storeadapter.ErrorKeyComparisonFailed))
				node, _ := adapter.Get("/foo")
				Expect(node).To(Equal(nodeFoo))
			})
		})

		Context("when the Value of the existing node is identical", func() {
			BeforeEach(func() {
				adapter.Create(nodeFoo)
			})

			It("deletes the existing node and returns nil", func() {
				err := adapter.CompareAndDelete(nodeFoo)
				Expect(err).NotTo(HaveOccurred())
				_, err = adapter.Get("/foo")
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))
			})
		})
	})

	Describe("Compare-and-Swapping", func() {
		Context("when the key is missing", func() {
			It("returns a KeyNotFound error", func() {
				node := storeadapter.StoreNode{
					Key:   "not/a/real/key",
					Value: []byte("value"),
				}

				err := adapter.CompareAndSwap(node, node)
				Expect(err).To(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when the key is present", func() {
			var (
				nodeFoo storeadapter.StoreNode
				nodeBar storeadapter.StoreNode
			)

			BeforeEach(func() {
				nodeFoo = storeadapter.StoreNode{
					Key:   "/foo",
					Value: []byte("foo"),
					TTL:   1,
				}

				nodeBar = storeadapter.StoreNode{
					Key:   "/foo",
					Value: []byte("bar"),
					TTL:   2,
				}

				adapter.Create(nodeFoo)
			})

			Context("and the Value of oldNode is different", func() {
				It("returns a KeyComparisonFailed error", func() {
					err := adapter.CompareAndSwap(nodeBar, nodeBar)
					Expect(err).To(Equal(storeadapter.ErrorKeyComparisonFailed))
				})

				It("does not update the existing node", func() {
					adapter.CompareAndSwap(nodeBar, nodeBar)

					retrievedNode, err := adapter.Get("/foo")
					Expect(err).NotTo(HaveOccurred())
					Expect(retrievedNode).To(MatchStoreNode(nodeFoo))
				})
			})

			Context("and the Value of oldNode is identical", func() {
				It("updates the node with the new node", func() {
					err := adapter.CompareAndSwap(nodeFoo, nodeBar)
					Expect(err).NotTo(HaveOccurred())

					retrievedNode, err := adapter.Get("/foo")
					Expect(err).NotTo(HaveOccurred())
					Expect(retrievedNode).To(MatchStoreNode(nodeBar))
				})
			})
		})
	})

	Describe("Watching", func() {
		Context("when a node under the key is created", func() {
			It("sends an event with CreateEvent type and the node's value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.Create(storeadapter.StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())
				event := <-events

				Expect(event.Type).To(Equal(storeadapter.CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is updated", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]storeadapter.StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with UpdateEvent type and the node's value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.SetMulti([]storeadapter.StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("new value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(storeadapter.UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is deleted", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]storeadapter.StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with DeleteEvent type and the node's previous value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.Delete("/foo/a")
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(storeadapter.DeleteEvent))
				Expect(event.PrevNode.Key).To(Equal("/foo/a"))
				Expect(string(event.PrevNode.Value)).To(Equal("some value"))

				close(done)
			}, 5.0)
		})

		Context("when a watch error occurs", func() {
			It("returns it to the watcher over the errs channel", func(done Done) {
				_, _, errs := adapter.Watch("/foo")

				disaster := errors.New("oh no!")

				adapter.WatchErrChannel <- disaster

				Expect(<-errs).To(Equal(disaster))

				close(done)
			})
		})
	})
})
