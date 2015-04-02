package etcdstoreadapter_test

import (
	"fmt"
	"time"

	"github.com/cloudfoundry/gunk/workpool"
	. "github.com/cloudfoundry/storeadapter"
	. "github.com/cloudfoundry/storeadapter/etcdstoreadapter"
	. "github.com/cloudfoundry/storeadapter/storenodematchers"
	"github.com/cloudfoundry/storeadapter/test_helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var counter = 0

var _ = Describe("ETCD Store Adapter", func() {
	var (
		adapter       StoreAdapter
		breakfastNode StoreNode
		lunchNode     StoreNode
	)

	BeforeEach(func() {
		breakfastNode = StoreNode{
			Key:   "/menu/breakfast",
			Value: []byte("waffles"),
		}

		lunchNode = StoreNode{
			Key:   "/menu/lunch",
			Value: []byte("burgers"),
		}

		adapter = NewETCDStoreAdapter(etcdRunner.NodeURLS(),
			workpool.NewWorkPool(10))
		err := adapter.Connect()
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		adapter.Disconnect()
	})

	Describe("Get", func() {
		BeforeEach(func() {
			err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when getting a key", func() {
			It("should return the appropriate store breakfastNode", func() {
				value, err := adapter.Get("/menu/breakfast")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(value).Should(MatchStoreNode(breakfastNode))
				Ω(value.Index).ShouldNot(BeZero())
			})
		})

		Context("When getting a non-existent key", func() {
			It("should return an error", func() {
				value, err := adapter.Get("/not_a_key")
				Ω(err).Should(Equal(ErrorKeyNotFound))
				Ω(value).Should(BeZero())
			})
		})

		Context("when getting a directory", func() {
			It("should return an error", func() {
				value, err := adapter.Get("/menu")
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
				Ω(value).Should(BeZero())
			})
		})

		Context("when the store is down", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return a timeout error", func() {
				value, err := adapter.Get("/foo/bar")
				Ω(err).Should(Equal(ErrorTimeout))
				Ω(value).Should(BeZero())
			})
		})
	})

	Describe("SetMulti", func() {
		It("should be able to set multiple things to the store at once", func() {
			err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
			Ω(err).ShouldNot(HaveOccurred())

			menu, err := adapter.ListRecursively("/menu")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(menu.ChildNodes).Should(HaveLen(2))
			Ω(menu.ChildNodes).Should(ContainElement(MatchStoreNode(breakfastNode)))
			Ω(menu.ChildNodes).Should(ContainElement(MatchStoreNode(lunchNode)))
		})

		Context("Setting to an existing node", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should be able to update existing entries", func() {
				lunchNode.Value = []byte("steak")
				err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
				Ω(err).ShouldNot(HaveOccurred())

				menu, err := adapter.ListRecursively("/menu")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(menu.ChildNodes).Should(HaveLen(2))
				Ω(menu.ChildNodes).Should(ContainElement(MatchStoreNode(breakfastNode)))
				Ω(menu.ChildNodes).Should(ContainElement(MatchStoreNode(lunchNode)))
			})

			It("should error when attempting to set to a directory", func() {
				dirNode := StoreNode{
					Key:   "/menu",
					Value: []byte("oops!"),
				}

				err := adapter.SetMulti([]StoreNode{dirNode})
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})

		Context("when the store is down", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return a timeout error", func() {
				err := adapter.SetMulti([]StoreNode{breakfastNode})
				Ω(err).Should(Equal(ErrorTimeout))
			})
		})
	})

	Describe("List", func() {
		BeforeEach(func() {
			err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("When listing a directory", func() {
			It("Should list directory contents", func() {
				value, err := adapter.ListRecursively("/menu")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(value.Key).Should(Equal("/menu"))
				Ω(value.Dir).Should(BeTrue())
				Ω(value.ChildNodes).Should(HaveLen(2))
				Ω(value.ChildNodes[0].Index).ShouldNot(BeZero())
				Ω(value.ChildNodes).Should(ContainElement(MatchStoreNode(breakfastNode)))
				Ω(value.ChildNodes).Should(ContainElement(MatchStoreNode(lunchNode)))
			})
		})

		Context("when listing a directory that contains directories", func() {
			var (
				firstCourseDinnerNode  StoreNode
				secondCourseDinnerNode StoreNode
			)

			BeforeEach(func() {
				firstCourseDinnerNode = StoreNode{
					Key:   "/menu/dinner/first_course",
					Value: []byte("Salad"),
				}
				secondCourseDinnerNode = StoreNode{
					Key:   "/menu/dinner/second_course",
					Value: []byte("Brisket"),
				}
				err := adapter.SetMulti([]StoreNode{firstCourseDinnerNode, secondCourseDinnerNode})

				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("when listing the root directory", func() {
				It("should list the contents recursively", func() {
					value, err := adapter.ListRecursively("/")
					Ω(err).ShouldNot(HaveOccurred())
					Ω(value.Key).Should(Equal("/"))
					Ω(value.Dir).Should(BeTrue())
					Ω(value.ChildNodes).Should(HaveLen(1))
					menuNode := value.ChildNodes[0]
					Ω(menuNode.Key).Should(Equal("/menu"))
					Ω(menuNode.Value).Should(BeEmpty())
					Ω(menuNode.Dir).Should(BeTrue())
					Ω(menuNode.ChildNodes).Should(HaveLen(3))
					Ω(menuNode.ChildNodes).Should(ContainElement(MatchStoreNode(breakfastNode)))
					Ω(menuNode.ChildNodes).Should(ContainElement(MatchStoreNode(lunchNode)))

					var dinnerNode StoreNode
					for _, node := range menuNode.ChildNodes {
						if node.Key == "/menu/dinner" {
							dinnerNode = node
							break
						}
					}
					Ω(dinnerNode.Dir).Should(BeTrue())
					Ω(dinnerNode.ChildNodes).Should(ContainElement(MatchStoreNode(firstCourseDinnerNode)))
					Ω(dinnerNode.ChildNodes).Should(ContainElement(MatchStoreNode(secondCourseDinnerNode)))
				})
			})

			Context("when listing another directory", func() {
				It("should list the contents recursively", func() {
					menuNode, err := adapter.ListRecursively("/menu")
					Ω(err).ShouldNot(HaveOccurred())
					Ω(menuNode.Key).Should(Equal("/menu"))
					Ω(menuNode.Value).Should(BeEmpty())
					Ω(menuNode.Dir).Should(BeTrue())
					Ω(menuNode.ChildNodes).Should(HaveLen(3))
					Ω(menuNode.ChildNodes).Should(ContainElement(MatchStoreNode(breakfastNode)))
					Ω(menuNode.ChildNodes).Should(ContainElement(MatchStoreNode(lunchNode)))

					var dinnerNode StoreNode
					for _, node := range menuNode.ChildNodes {
						if node.Key == "/menu/dinner" {
							dinnerNode = node
							break
						}
					}
					Ω(dinnerNode.Dir).Should(BeTrue())
					Ω(dinnerNode.ChildNodes).Should(ContainElement(MatchStoreNode(firstCourseDinnerNode)))
					Ω(dinnerNode.ChildNodes).Should(ContainElement(MatchStoreNode(secondCourseDinnerNode)))
				})
			})
		})

		Context("when listing an empty directory", func() {
			It("should return an empty list of breakfastNodes, and not error", func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/empty_dir/temp",
						Value: []byte("foo"),
					},
				})
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.Delete("/empty_dir/temp")
				Ω(err).ShouldNot(HaveOccurred())

				value, err := adapter.ListRecursively("/empty_dir")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(value.Key).Should(Equal("/empty_dir"))
				Ω(value.Dir).Should(BeTrue())
				Ω(value.ChildNodes).Should(HaveLen(0))
			})
		})

		Context("when listing a non-existent key", func() {
			It("should return an error", func() {
				value, err := adapter.ListRecursively("/nothing-here")
				Ω(err).Should(Equal(ErrorKeyNotFound))
				Ω(value).Should(BeZero())
			})
		})

		Context("when listing an entry", func() {
			It("should return an error", func() {
				value, err := adapter.ListRecursively("/menu/breakfast")
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(ErrorNodeIsNotDirectory))
				Ω(value).Should(BeZero())
			})
		})

		Context("when the store is down", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return a timeout error", func() {
				value, err := adapter.ListRecursively("/menu")
				Ω(err).Should(Equal(ErrorTimeout))
				Ω(value).Should(BeZero())
			})
		})
	})

	Describe("Delete", func() {
		BeforeEach(func() {
			err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when deleting existing keys", func() {
			It("should delete the keys", func() {
				err := adapter.Delete("/menu/breakfast", "/menu/lunch")
				Ω(err).ShouldNot(HaveOccurred())

				value, err := adapter.Get("/menu/breakfast")
				Ω(err).Should(Equal(ErrorKeyNotFound))
				Ω(value).Should(BeZero())

				value, err = adapter.Get("/menu/lunch")
				Ω(err).Should(Equal(ErrorKeyNotFound))
				Ω(value).Should(BeZero())
			})
		})

		Context("when deleting a non-existing key", func() {
			It("should error", func() {
				err := adapter.Delete("/not-a-key")
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when deleting a directory", func() {
			It("deletes the key and its contents", func() {
				err := adapter.Delete("/menu")
				Ω(err).ShouldNot(HaveOccurred())

				_, err = adapter.Get("/menu/breakfast")
				Ω(err).Should(Equal(ErrorKeyNotFound))

				_, err = adapter.Get("/menu")
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when the store is down", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return a timeout error", func() {
				err := adapter.Delete("/menu/breakfast")
				Ω(err).Should(Equal(ErrorTimeout))
			})
		})
	})

	Describe("Comparing-and-deleting", func() {
		var nodeFoo StoreNode
		var nodeBar StoreNode

		BeforeEach(func() {
			nodeFoo = StoreNode{Key: "/foo", Value: []byte("some foo value")}
			nodeBar = StoreNode{Key: "/bar", Value: []byte("some bar value")}
		})

		Context("when nodes exist in the store", func() {
			BeforeEach(func() {
				err := adapter.Create(nodeFoo)
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.Create(nodeBar)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("deletes the given nodes", func() {
				err := adapter.CompareAndDelete(nodeFoo, nodeBar)
				Ω(err).ShouldNot(HaveOccurred())

				_, err = adapter.Get(nodeFoo.Key)
				Ω(err).Should(Equal(ErrorKeyNotFound))

				_, err = adapter.Get(nodeBar.Key)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})

			Context("but the comparison fails for one node", func() {
				BeforeEach(func() {
					nodeFoo.Value = []byte("some mismatched foo value")
				})

				It("returns an error", func() {
					err := adapter.CompareAndDelete(nodeFoo, nodeBar)
					Ω(err).Should(Equal(ErrorKeyComparisonFailed))

					_, err = adapter.Get(nodeFoo.Key)
					Ω(err).ShouldNot(HaveOccurred())

					_, err = adapter.Get(nodeBar.Key)
					Ω(err).Should(Equal(ErrorKeyNotFound))
				})
			})
		})

		Context("when a node does not exist at the key", func() {
			It("returns an error", func() {
				err := adapter.CompareAndDelete(nodeFoo)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				parentNode := StoreNode{Key: "/dir", Value: []byte("some value")}

				err = adapter.CompareAndDelete(parentNode)
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})
	})

	Describe("Comparing-and-deleting-by-index", func() {
		var nodeFoo StoreNode
		var nodeBar StoreNode

		BeforeEach(func() {
			nodeFoo = StoreNode{Key: "/foo", Value: []byte("some foo value")}
			nodeBar = StoreNode{Key: "/bar", Value: []byte("some bar value")}
		})

		Context("when nodes exist in the store", func() {
			var etcdNodeFoo StoreNode
			var etcdNodeBar StoreNode

			BeforeEach(func() {
				err := adapter.Create(nodeFoo)
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.Create(nodeBar)
				Ω(err).ShouldNot(HaveOccurred())

				etcdNodeFoo, err = adapter.Get(nodeFoo.Key)
				Ω(err).ShouldNot(HaveOccurred())

				etcdNodeBar, err = adapter.Get(nodeBar.Key)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("deletes the given nodes", func() {
				err := adapter.CompareAndDeleteByIndex(etcdNodeFoo, etcdNodeBar)
				Ω(err).ShouldNot(HaveOccurred())

				_, err = adapter.Get(nodeFoo.Key)
				Ω(err).Should(Equal(ErrorKeyNotFound))

				_, err = adapter.Get(nodeBar.Key)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})

			Context("but the comparison fails for one node", func() {
				It("returns an error", func() {
					err := adapter.CompareAndSwap(nodeFoo, nodeFoo)
					Ω(err).ShouldNot(HaveOccurred())

					err = adapter.CompareAndDeleteByIndex(etcdNodeFoo, etcdNodeBar)
					Ω(err).Should(Equal(ErrorKeyComparisonFailed))

					_, err = adapter.Get(nodeFoo.Key)
					Ω(err).ShouldNot(HaveOccurred())

					_, err = adapter.Get(nodeBar.Key)
					Ω(err).Should(Equal(ErrorKeyNotFound))
				})
			})
		})

		Context("when a node does not exist at the key", func() {
			BeforeEach(func() {
				nodeFoo.Index = 1234
			})

			It("returns an error", func() {
				err := adapter.CompareAndDeleteByIndex(nodeFoo)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				parentNode, err := adapter.ListRecursively("/dir")
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.CompareAndDeleteByIndex(parentNode)
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})
	})

	Context("When setting a key with a non-zero TTL", func() {
		It("should stay in the store for the duration of its TTL and then disappear", func() {
			breakfastNode.TTL = 1
			err := adapter.SetMulti([]StoreNode{breakfastNode})
			Ω(err).ShouldNot(HaveOccurred())

			_, err = adapter.Get("/menu/breakfast")
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(func() interface{} {
				_, err = adapter.Get("/menu/breakfast")
				return err
			}, 2, 0.01).Should(Equal(ErrorKeyNotFound)) // as of etcd v0.2rc1, etcd seems to take an extra 0.5 seconds to expire its TTLs
		})
	})

	Describe("Maintaining a node's presence (and lack thereof)", func() {
		var (
			uniqueStoreNodeForThisTest StoreNode //avoid collisions between test runs
		)

		releaseMaintainedNode := func(release chan chan bool) {
			waiting := make(chan bool)
			release <- waiting
			Eventually(waiting).Should(BeClosed())
		}

		waitTilLocked := func(storeNode StoreNode) chan chan bool {
			nodeStatus, releaseLock, _ := adapter.MaintainNode(storeNode)

			reporter := test_helpers.NewStatusReporter(nodeStatus)
			Eventually(reporter.Reporting).Should(BeTrue())
			Eventually(reporter.Locked).Should(BeTrue())

			return releaseLock
		}

		BeforeEach(func() {
			uniqueStoreNodeForThisTest = StoreNode{
				Key: fmt.Sprintf("analyzer-%d", counter),
				TTL: 1,
			}

			counter++
		})

		Context("when passed a TTL of 0", func() {
			It("should be like, no way man", func() {
				uniqueStoreNodeForThisTest.TTL = 0

				nodeStatus, releaseLock, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
				Ω(err).Should(Equal(ErrorInvalidTTL))
				Ω(nodeStatus).Should(BeNil())
				Ω(releaseLock).Should(BeNil())
			})
		})

		Context("when the store is not available", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("no status is received", func() {
				nodeStatus, releaseLock, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
				Ω(err).Should(BeNil())
				Ω(releaseLock).ShouldNot(BeNil())
				Consistently(nodeStatus, 2).ShouldNot(Receive())

				releaseMaintainedNode(releaseLock)
			})
		})

		Context("when the lock is available", func() {
			It("receive a status of true on the TTL requested", func() {
				nodeStatus, releaseLock, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(nodeStatus).ShouldNot(BeNil())
				Ω(releaseLock).ShouldNot(BeNil())

				var status bool
				Eventually(nodeStatus, 2.0).Should(Receive(&status))
				Ω(status).Should(BeTrue())

				start := time.Now()
				Eventually(nodeStatus, 2.0).Should(Receive(&status))
				Ω(status).Should(BeTrue())
				Ω(time.Now().Sub(start)).Should(BeNumerically("==", 1*time.Second, 400*time.Millisecond))

				releaseMaintainedNode(releaseLock)
			})

			It("should maintain the lock in the background", func() {
				releaseLock1 := waitTilLocked(uniqueStoreNodeForThisTest)

				otherUniqueStoreNodeForThisTest := uniqueStoreNodeForThisTest
				otherUniqueStoreNodeForThisTest.Value = []byte("other")

				nodeStatus2, releaseLock2, _ := adapter.MaintainNode(otherUniqueStoreNodeForThisTest)

				reporter := test_helpers.NewStatusReporter(nodeStatus2)
				Consistently(reporter.Reporting, 2).Should(BeFalse())

				releaseMaintainedNode(releaseLock1)

				releaseMaintainedNode(releaseLock2)
			})

			Context("when a value is given", func() {
				BeforeEach(func() {
					uniqueStoreNodeForThisTest.Value = []byte("some value")
				})

				It("creates the lock with the given value", func() {
					nodeStatus, release, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
					Ω(err).ShouldNot(HaveOccurred())
					Eventually(nodeStatus).Should(Receive())

					val, err := adapter.Get(uniqueStoreNodeForThisTest.Key)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(string(val.Value)).Should(Equal("some value"))

					releaseMaintainedNode(release)
				})
			})

			Context("when a value is NOT given", func() {
				It("creates the lock with some unique value", func() {
					otherUniqueStoreNodeForThisTest := uniqueStoreNodeForThisTest
					otherUniqueStoreNodeForThisTest.Key = otherUniqueStoreNodeForThisTest.Key + "other"

					releaseLock1 := waitTilLocked(uniqueStoreNodeForThisTest)
					defer releaseMaintainedNode(releaseLock1)

					val, err := adapter.Get(uniqueStoreNodeForThisTest.Key)
					Ω(err).ShouldNot(HaveOccurred())

					releaseLock2 := waitTilLocked(otherUniqueStoreNodeForThisTest)
					defer releaseMaintainedNode(releaseLock2)

					otherval, err := adapter.Get(otherUniqueStoreNodeForThisTest.Key)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(string(val.Value)).ShouldNot(Equal(string(otherval.Value)))
				})
			})

			Context("when the lock disappears after it has been acquired (e.g. ETCD store is reset)", func() {
				AfterEach(func() {
					etcdRunner.Start()
				})

				It("should send a false down the status channel", func() {
					nodeStatus, release, _ := adapter.MaintainNode(uniqueStoreNodeForThisTest)
					Eventually(nodeStatus).Should(Receive())

					etcdRunner.Stop()

					var status bool
					Eventually(nodeStatus).Should(Receive(&status))
					Ω(status).Should(BeFalse())

					releaseMaintainedNode(release)
				})
			})
		})

		Context("when releasing the lock", func() {
			It("makes it available for others trying to acquire it", func() {
				releaseLock1 := waitTilLocked(uniqueStoreNodeForThisTest)

				otherStoreNodeForThisTest := uniqueStoreNodeForThisTest
				otherStoreNodeForThisTest.Value = []byte("other")

				nodeStatus2, releaseLock2, err2 := adapter.MaintainNode(otherStoreNodeForThisTest)
				Ω(err2).ShouldNot(HaveOccurred())

				Consistently(nodeStatus2).ShouldNot(Receive())

				releaseMaintainedNode(releaseLock1)

				var status bool
				Eventually(nodeStatus2, 2.0).Should(Receive(&status))
				Ω(status).Should(BeTrue())

				releaseMaintainedNode(releaseLock2)
			})

			It("deletes the lock's key", func() {
				done := waitTilLocked(uniqueStoreNodeForThisTest)

				_, err := adapter.Get(uniqueStoreNodeForThisTest.Key)
				Ω(err).ShouldNot(HaveOccurred())

				waiting := make(chan bool)
				done <- waiting
				<-waiting

				_, err = adapter.Get(uniqueStoreNodeForThisTest.Key)
				Ω(err).Should(HaveOccurred())
			})

			It("the status channel is closed", func() {
				nodeStatus, releaseLock, _ := adapter.MaintainNode(uniqueStoreNodeForThisTest)

				reporter := test_helpers.NewStatusReporter(nodeStatus)

				Eventually(reporter.Locked).Should(BeTrue())

				releaseLock <- nil

				Eventually(reporter.Reporting).Should(BeFalse())
			})

		})
	})

	Describe("Creating", func() {
		var node StoreNode

		BeforeEach(func() {
			node = StoreNode{Key: "/foo", Value: []byte("some value")}
			err := adapter.Create(node)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("creates the node at the given key", func() {
			retrievedNode, err := adapter.Get("/foo")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(retrievedNode).Should(MatchStoreNode(node))
		})

		Context("when a node already exists at the key", func() {
			It("returns an error", func() {
				err := adapter.Create(node)
				Ω(err).Should(Equal(ErrorKeyExists))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.Create(StoreNode{Key: "/dir", Value: []byte("some value")})
				Ω(err).Should(Equal(ErrorKeyExists))
			})
		})
	})

	Describe("Updating", func() {
		var node StoreNode

		BeforeEach(func() {
			node = StoreNode{Key: "/foo", Value: []byte("some value")}
		})

		It("updates the existing node at the given key", func() {
			err := adapter.Create(node)
			Ω(err).ShouldNot(HaveOccurred())

			node.Value = []byte("some new value")

			err = adapter.Update(node)
			Ω(err).ShouldNot(HaveOccurred())

			retrievedNode, err := adapter.Get("/foo")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(retrievedNode).Should(MatchStoreNode(node))
		})

		Context("when a node does not exist at the key", func() {
			It("returns an error", func() {
				err := adapter.Update(node)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.Update(StoreNode{Key: "/dir", Value: []byte("some value")})
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})
	})

	Describe("Comparing-and-swapping", func() {
		var node StoreNode

		BeforeEach(func() {
			node = StoreNode{Key: "/foo", Value: []byte("some value")}
		})

		It("updates the existing node at the given key", func() {
			err := adapter.Create(node)
			Ω(err).ShouldNot(HaveOccurred())

			newNode := node
			newNode.Value = []byte("some new value")

			err = adapter.CompareAndSwap(node, newNode)
			Ω(err).ShouldNot(HaveOccurred())

			retrievedNode, err := adapter.Get("/foo")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(retrievedNode).Should(MatchStoreNode(newNode))
		})

		Context("when a node exists but the comparison fails", func() {
			It("returns an error", func() {
				err := adapter.Create(node)
				Ω(err).ShouldNot(HaveOccurred())

				wrongNode := node
				wrongNode.Value = []byte("NOPE")

				newNode := node
				newNode.Value = []byte("some new value")

				err = adapter.CompareAndSwap(wrongNode, newNode)
				Ω(err).Should(Equal(ErrorKeyComparisonFailed))

				retrievedNode, err := adapter.Get("/foo")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(retrievedNode).Should(MatchStoreNode(node))
			})
		})

		Context("when a node does not exist at the key", func() {
			It("returns an error", func() {
				err := adapter.CompareAndSwap(node, node)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				newNode := StoreNode{Key: "/dir", Value: []byte("some value")}

				err = adapter.CompareAndSwap(newNode, newNode)
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})
	})

	Describe("Comparing-and-swapping by index", func() {
		var node StoreNode

		BeforeEach(func() {
			node = StoreNode{Key: "/foo", Value: []byte("some value")}
		})

		It("updates the existing node at the given key", func() {
			err := adapter.Create(node)
			Ω(err).ShouldNot(HaveOccurred())

			newNode := node
			newNode.Value = []byte("some new value")

			etcd_node, err := adapter.Get("/foo")
			Ω(err).ShouldNot(HaveOccurred())

			err = adapter.CompareAndSwapByIndex(etcd_node.Index, newNode)
			Ω(err).ShouldNot(HaveOccurred())

			retrievedNode, err := adapter.Get("/foo")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(retrievedNode).Should(MatchStoreNode(newNode))
		})

		Context("when a node exists but the comparison fails", func() {
			It("returns an error", func() {
				err := adapter.Create(node)
				Ω(err).ShouldNot(HaveOccurred())

				newNode := node
				newNode.Value = []byte("some new value")

				err = adapter.CompareAndSwapByIndex(4271138, newNode)
				Ω(err).Should(Equal(ErrorKeyComparisonFailed))

				retrievedNode, err := adapter.Get("/foo")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(retrievedNode).Should(MatchStoreNode(node))
			})
		})

		Context("when a node does not exist at the key", func() {
			It("returns an error", func() {
				err := adapter.CompareAndSwapByIndex(4271338, node)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				newNode := StoreNode{Key: "/dir", Value: []byte("some value")}

				err = adapter.CompareAndSwapByIndex(4271338, newNode)
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})
	})

	Describe("Watching", func() {
		Context("when a node under the key is created", func() {
			It("sends an event with CreateEvent type and the node's value, and no previous node", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.Create(StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))
				Expect(event.PrevNode).To(BeZero())

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is set", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with UpdateEvent type and the node's value, and the previous node", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("new value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))
				Expect(event.PrevNode.Key).To(Equal("/foo/a"))
				Expect(string(event.PrevNode.Value)).To(Equal("some value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is updated, and the previous node", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with UpdateEvent type and the node's value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.UpdateDirTTL("/foo", 10)
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo"))
				Expect(event.Node.TTL).To(BeNumerically("==", 10))
				Expect(event.PrevNode.Key).To(Equal("/foo"))
				Expect(event.PrevNode.TTL).To(BeNumerically("==", 0))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is compare and swapped", func() {
			var node StoreNode
			BeforeEach(func() {
				node = StoreNode{
					Key:   "/foo/a",
					Value: []byte("some value"),
				}
				err := adapter.SetMulti([]StoreNode{node})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with UpdateEvent type and the node's value, and the previous node", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				newNode := node
				newNode.Value = []byte("new value")
				err := adapter.CompareAndSwap(node, newNode)
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(*event.Node).To(MatchStoreNode(newNode))
				Expect(event.PrevNode.Key).To(Equal("/foo/a"))
				Expect(string(event.PrevNode.Value)).To(Equal("some value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is deleted", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with DeleteEvent type and the previous node", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.Delete("/foo/a")
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(DeleteEvent))
				Expect(event.Node).To(BeNil())
				Expect(event.PrevNode.Key).To(Equal("/foo/a"))
				Expect(string(event.PrevNode.Value)).To(Equal("some value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is compare-and-deleted", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with DeleteEvent type and the previous node", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.CompareAndDelete(StoreNode{
					Key:   "/foo/a",
					Value: []byte("some value"),
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(DeleteEvent))
				Expect(event.Node).To(BeNil())
				Expect(event.PrevNode.Key).To(Equal("/foo/a"))
				Expect(string(event.PrevNode.Value)).To(Equal("some value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key expires", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
						TTL:   1,
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with ExpireEvent type and the previous node", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				time.Sleep(2 * time.Second)

				event := <-events
				Expect(event.Type).To(Equal(ExpireEvent))
				Expect(event.Node).To(BeNil())
				Expect(event.PrevNode.Key).To(Equal("/foo/a"))
				Expect(string(event.PrevNode.Value)).To(Equal("some value"))

				close(done)
			}, 5.0)
		})

		Context("when told to stop watching", func() {
			It("no longer notifies for any events", func(done Done) {
				events, stop, errors := adapter.Watch("/foo")

				err := adapter.Create(StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))

				stop <- true
				Eventually(events, 2).Should(BeClosed())
				Eventually(errors, 2).Should(BeClosed())

				close(done)
			}, 5.0)
		})

		Context("when told to disconnect", func() {
			It("no longer notifies for any events", func() {
				events, _, errors := adapter.Watch("/foo")

				adapter.Disconnect()

				Eventually(events).Should(BeClosed())
				Eventually(errors).Should(BeClosed())
			})
		})

		Context("when 1000 (current etcd constant) events occur between the start index and now", func() {
			It("skips the missing event history and eventually catches up", func() {
				events, _, errChan := adapter.Watch("/foo")

				err := adapter.Create(StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))

				for i, _ := range make([]bool, 1003) {
					err := adapter.SetMulti([]StoreNode{
						{
							Key:   "/foo/a",
							Value: []byte(fmt.Sprintf("%d", i+1)),
						},
					})
					Expect(err).ToNot(HaveOccurred())
				}

				Eventually(events).Should(Receive(&event))
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("1"))

				// all events will be missed while we're not reading them
				Consistently(events).ShouldNot(Receive())

				err = adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("fast-forwarded index"),
					},
				})
				Expect(err).ToNot(HaveOccurred())

				Eventually(events).Should(Receive(&event))
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("fast-forwarded index"))

				Expect(errChan).To(BeEmpty())
			})
		})

		Context("when etcd disappears mid-watch", func() {
			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should write to the error channel", func(done Done) {
				events, _, errChan := adapter.Watch("/foo")

				err := adapter.Create(StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())

				etcdRunner.Stop()

				event := <-events
				Expect(event.Type).To(Equal(CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))

				Ω(<-errChan).Should(Equal(ErrorTimeout))

				close(done)
			}, 5)
		})
	})

	Describe("UpdateDirTTL", func() {
		Context("When the directory exists", func() {
			It("should set the TTL", func() {
				err := adapter.Create(breakfastNode)
				Expect(err).NotTo(HaveOccurred())

				err = adapter.UpdateDirTTL("/menu", 1)
				Expect(err).NotTo(HaveOccurred())

				node, err := adapter.ListRecursively("/menu")
				Expect(err).NotTo(HaveOccurred())
				Expect(node.TTL).NotTo(BeZero())

				_, err = adapter.Get("/menu/breakfast")
				Expect(err).NotTo(HaveOccurred())

				time.Sleep(2 * time.Second)

				_, err = adapter.Get("/menu/breakfast")
				Expect(err).To(Equal(ErrorKeyNotFound))
			})
		})

		Context("When the directory does not exist", func() {
			It("should return a ErrorKeyNotFound", func() {
				err := adapter.UpdateDirTTL("/non-existent-key", 1)
				Expect(err).To(Equal(ErrorKeyNotFound))
			})
		})

		Context("When the key represents a leaf, not a directory", func() {
			It("should return a ErrorNodeIsNotDirectory error", func() {
				err := adapter.Create(breakfastNode)
				Expect(err).NotTo(HaveOccurred())

				err = adapter.UpdateDirTTL("/menu/breakfast", 1)
				Expect(err).To(Equal(ErrorNodeIsNotDirectory))
			})
		})
	})
})
