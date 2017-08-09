package container_test

import (
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/routing-api/models"

	"code.cloudfoundry.org/gorouter/registry/container"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Trie", func() {

	var (
		r      *container.Trie
		modTag models.ModificationTag
	)

	BeforeEach(func() {
		r = container.NewTrie()
		modTag = models.ModificationTag{}
	})

	Describe(".Find", func() {
		It("works for the root node", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/", p)
			node := r.Find("/")
			Expect(node).To(Equal(p))
		})

		It("finds an exact match to an existing key", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo/bar", p)
			node := r.Find("/foo/bar")
			Expect(node).To(Equal(p))
		})

		It("returns nil when no exact match is found", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p)
			node := r.Find("/foo/bar")
			Expect(node).To(BeNil())
		})

		It("returns nil if a shorter path exists", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo/bar", p)
			node := r.Find("/foo/bar/baz")
			Expect(node).To(BeNil())
		})
	})

	Describe(".MatchUri", func() {
		It("works for the root node", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/", p)
			node := r.MatchUri("/")
			Expect(node).To(Equal(p))
		})

		It("finds a existing key", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo/bar", p)
			node := r.MatchUri("/foo/bar")
			Expect(node).To(Equal(p))
		})

		It("finds a matching shorter key", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo/bar", p)
			node := r.MatchUri("/foo/bar/baz")
			Expect(node).To(Equal(p))
		})

		It("returns nil when no match found", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p)
			node := r.MatchUri("/foo/bar")
			Expect(node).To(BeNil())
		})

		It("returns the longest found match when routes overlap", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo", p1)
			r.Insert("/foo/bar/baz", p2)
			node := r.MatchUri("/foo/bar")
			Expect(node).To(Equal(p1))
		})

		It("returns the longest found match when routes overlap and longer path created first", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p2)
			r.Insert("/foo", p1)
			node := r.MatchUri("/foo/bar")
			Expect(node).To(Equal(p1))
		})
	})

	Describe(".Insert", func() {
		It("adds a non-existing key", func() {
			p := route.NewPool(0, "", "")
			childBar := r.Insert("/foo/bar", p)

			trie0 := r
			Expect(len(trie0.ChildNodes)).To(Equal(1))
			child0 := trie0.ChildNodes["foo"]
			Expect(child0.Segment).To(Equal("foo"))
			Expect(len(child0.ChildNodes)).To(Equal(1))

			child1 := child0.ChildNodes["bar"]
			Expect(child1.Segment).To(Equal("bar"))
			Expect(child1.Pool).To(Equal(p))
			Expect(len(child1.ChildNodes)).To(Equal(0))

			Expect(child1).To(Equal(childBar))
		})

		It("adds a child node", func() {
			rootPool := route.NewPool(0, "", "")
			childPool := route.NewPool(0, "", "")

			_ = r.Insert("example", rootPool)

			baseNode := r
			Expect(len(baseNode.ChildNodes)).To(Equal(1))
			exampleNode := baseNode.ChildNodes["example"]
			Expect(exampleNode.Segment).To(Equal("example"))

			_ = r.Insert("example/bar", childPool)

			Expect(len(exampleNode.ChildNodes)).To(Equal(1))
		})
	})

	Describe(".Delete", func() {
		It("removes a pool", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo", p1)
			r.Insert("/foo/bar", p2)

			ok := r.Delete("/foo")
			Expect(ok).To(BeTrue())
			pool := r.MatchUri("/foo")
			Expect(pool).To(BeNil())
			pool = r.MatchUri("/foo/bar")
			Expect(pool).NotTo(BeNil())
		})

		It("cleans up the node", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo", p)

			r.Delete("/foo")
			Expect(r.ChildNodes).To(BeEmpty())
		})

		It("cleans up parent nodes", func() {
			p := route.NewPool(42, "", "")
			r.Insert("example.com/foo/bar/baz", p)

			r.Delete("example.com/foo/bar/baz")
			Expect(r.ChildNodes).To(BeEmpty())
		})

		It("does not prune nodes with other children", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p1)
			r.Insert("/foo/something/baz", p2)

			r.Delete("/foo/bar/baz")
			pool := r.MatchUri("/foo/something/baz")
			Expect(pool).NotTo(BeNil())
		})

		It("does not prune nodes with pools", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p1)
			r.Insert("/foo/bar", p2)

			r.Delete("/foo/bar/baz")
			pool := r.MatchUri("/foo/bar")
			Expect(pool).NotTo(BeNil())
		})

		It("Returns the number of pools after deleting one", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p1)
			r.Insert("/foo/bar", p2)

			r.Delete("/foo/bar/baz")
			Expect(r.PoolCount()).To(Equal(1))
		})

		It("removes a pool", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo", p1)
			r.Insert("/foo/bar", p2)

			ok := r.Delete("/foo")
			Expect(ok).To(BeTrue())
			pool := r.MatchUri("/foo")
			Expect(pool).To(BeNil())
			pool = r.MatchUri("/foo/bar")
			Expect(pool).NotTo(BeNil())
		})

		It("cleans up the node", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo", p)

			r.Delete("/foo")
			Expect(r.ChildNodes).To(BeEmpty())
		})

		It("cleans up parent nodes", func() {
			p := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p)

			r.Delete("/foo/bar/baz")
			Expect(r.ChildNodes).To(BeEmpty())
		})

		It("does not prune nodes with other children", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p1)
			r.Insert("/foo/something/baz", p2)

			r.Delete("/foo/bar/baz")
			pool := r.MatchUri("/foo/something/baz")
			Expect(pool).NotTo(BeNil())
		})

		It("does not prune nodes with pools", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			r.Insert("/foo/bar/baz", p1)
			r.Insert("/foo/bar", p2)

			r.Delete("/foo/bar/baz")
			pool := r.MatchUri("/foo/bar")
			Expect(pool).NotTo(BeNil())
		})
	})

	It("Returns the number of pools", func() {
		Expect(r.PoolCount()).To(Equal(0))

		p1 := route.NewPool(42, "", "")
		p2 := route.NewPool(42, "", "")
		r.Insert("/foo/bar/baz", p1)
		r.Insert("/foo/bar", p2)

		Expect(r.PoolCount()).To(Equal(2))
	})

	Describe(".PruneDeadLeaves", func() {
		It("removes dead leaves", func() {
			segments := make([]string, 0)
			count := 0
			f := func(r *container.Trie) {
				segments = append(segments, r.Segment)
				count += 1
			}

			e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "192.168.1.1", 4321, "", "", nil, -1, "", modTag, "", false)
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			p3 := route.NewPool(42, "", "")
			p4 := route.NewPool(42, "", "")
			p1.Put(e1)
			p2.Put(e2)
			r.Insert("/foo", p1)
			r.Insert("/foo/bar/baz", p2)
			r.Insert("/zoo", p3)
			r.Insert("/foo/bar/zak", p4)

			r.EachNodeWithPool(f)
			Expect(segments).To(ConsistOf("foo", "baz", "zak", "zoo"))
			Expect(count).To(Equal(4))

			r.PruneDeadLeaves()
			segments = make([]string, 0)
			count = 0
			r.EachNodeWithPool(f)
			Expect(segments).To(ConsistOf("foo", "baz"))
			Expect(count).To(Equal(2))
		})
	})

	Describe(".Snip", func() {
		It("removes a branch from the trie", func() {
			e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag, "", false)
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			p1.Put(e1)

			fooNode := r.Insert("/foo", p1)
			bazNode := r.Insert("/foo/bar/baz", p2)
			zakNode := r.Insert("/foo/bar/zak", p2)
			barNode := fooNode.ChildNodes["bar"]

			Expect(barNode.ChildNodes).To(HaveLen(2))
			Expect(r.ChildNodes).To(HaveLen(1))

			zakNode.Snip()
			Expect(barNode.ChildNodes).To(HaveLen(1))
			Expect(r.ChildNodes).To(HaveLen(1))
			Expect(fooNode.ChildNodes).To(HaveLen(1))

			bazNode.Snip()
			Expect(fooNode.ChildNodes).To(HaveLen(0))
		})

		It("deletes empty pools", func() {
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag, "", false)
			p2.Put(e1)

			fooNode := r.Insert("/foo", p1)
			_ = r.Insert("/foo/bar", p2)

			fooNode.Snip()
			Expect(r.ChildNodes).To(HaveLen(1))
			Expect(fooNode.Pool).To(BeNil())
			Expect(fooNode.ChildNodes).To(HaveLen(1))
		})
	})

	Describe(".EndpointCount", func() {
		It("returns the number of endpoints", func() {
			Expect(r.EndpointCount()).To(Equal(0))

			e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "192.168.1.1", 4321, "", "", nil, -1, "", modTag, "", false)
			p := route.NewPool(42, "", "")
			p.Put(e1)
			p.Put(e2)
			r.Insert("/foo/bar", p)

			Expect(r.EndpointCount()).To(Equal(2))
		})

		It("counts the uniques leaf endpoints", func() {
			e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag, "", false)
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			p1.Put(e1)
			p2.Put(e2)
			r.Insert("/foo", p1)
			r.Insert("/foo/bar", p2)

			Expect(r.EndpointCount()).To(Equal(1))
		})
	})

	Describe(".ToMap", func() {
		It("Can be represented by a map", func() {
			e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "192.168.1.1", 4321, "", "", nil, -1, "", modTag, "", false)
			p1 := route.NewPool(42, "", "")
			p2 := route.NewPool(42, "", "")
			p1.Put(e1)
			p2.Put(e2)
			r.Insert("/foo", p1)
			r.Insert("/foo/bar/baz", p2)
			expectedMap := map[route.Uri]*route.Pool{
				"foo":         p1,
				"foo/bar/baz": p2,
			}

			Expect(r.ToMap()).To(Equal(expectedMap))
		})
	})

	Describe(".ToPath", func() {
		It("Returns full URI", func() {
			e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag, "", false)
			p1 := route.NewPool(42, "", "")
			p1.Put(e1)
			node1 := r.Insert("foo.com", p1)
			node2 := r.Insert("foo.io/bar/baz", p1)
			node3 := r.Insert("foo.io/bar/foo", p1)

			Expect(node1.ToPath()).To(Equal("foo.com"))
			Expect(node2.ToPath()).To(Equal("foo.io/bar/baz"))
			Expect(node3.ToPath()).To(Equal("foo.io/bar/foo"))
		})
	})

	It("applies a function to each node with a pool", func() {
		p1 := route.NewPool(42, "", "")
		p2 := route.NewPool(42, "", "")
		r.Insert("/foo", p1)
		r.Insert("/foo/bar/baz", p2)

		pools := make([]*route.Pool, 0)
		r.EachNodeWithPool(func(node *container.Trie) {
			pools = append(pools, node.Pool)
		})

		Expect(pools).To(HaveLen(2))
		Expect(pools).To(ContainElement(p1))
		Expect(pools).To(ContainElement(p2))
	})
})
