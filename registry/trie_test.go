package registry_test

import (
	"github.com/cloudfoundry/gorouter/route"

	. "github.com/cloudfoundry/gorouter/registry"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Trie", func() {

	var (
		r *Trie
	)

	BeforeEach(func() {
		r = NewTrie()
	})

	It("works for the root node", func() {
		p := route.NewPool(42)
		r.Insert("/", p)
		node, ok := r.Find("/")
		Expect(node).To(Equal(p))
		Expect(ok).To(BeTrue())
	})

	It("finds a existing key", func() {
		p := route.NewPool(42)
		r.Insert("/foo/bar", p)
		node, ok := r.Find("/foo/bar")
		Expect(node).To(Equal(p))
		Expect(ok).To(BeTrue())
	})

	It("finds a matching shorter key", func() {
		p := route.NewPool(42)
		r.Insert("/foo/bar", p)
		node, ok := r.Find("/foo/bar/baz")
		Expect(node).To(Equal(p))
		Expect(ok).To(BeTrue())
	})

	It("returns nil when no match found", func() {
		p := route.NewPool(42)
		r.Insert("/foo/bar/baz", p)
		node, ok := r.Find("/foo/bar")
		Expect(node).To(BeNil())
		Expect(ok).To(BeFalse())
	})

	It("returns the longest found match when routes overlap", func() {
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		r.Insert("/foo", p1)
		r.Insert("/foo/bar/baz", p2)
		node, ok := r.Find("/foo/bar")
		Expect(node).To(Equal(p1))
		Expect(ok).To(BeTrue())
	})

	It("returns the longest found match when routes overlap and longer path created first", func() {
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		r.Insert("/foo/bar/baz", p2)
		r.Insert("/foo", p1)
		node, ok := r.Find("/foo/bar")
		Expect(node).To(Equal(p1))
		Expect(ok).To(BeTrue())
	})

	It("adds a non-existing key", func() {
		p := route.NewPool(0)
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

	It("Delete removes a pool", func() {
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		r.Insert("/foo", p1)
		r.Insert("/foo/bar", p2)

		ok := r.Delete("/foo")
		Expect(ok).To(BeTrue())
		_, ok = r.Find("/foo")
		Expect(ok).To(BeFalse())
		_, ok = r.Find("/foo/bar")
		Expect(ok).To(BeTrue())
	})

	It("Delete cleans up the node", func() {
		p := route.NewPool(42)
		r.Insert("/foo", p)

		r.Delete("/foo")
		Expect(r.ChildNodes).To(BeEmpty())
	})

	It("Delete cleans up parent nodes", func() {
		p := route.NewPool(42)
		r.Insert("/foo/bar/baz", p)

		r.Delete("/foo/bar/baz")
		Expect(r.ChildNodes).To(BeEmpty())
	})

	It("Delete does not prune nodes with other children", func() {
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		r.Insert("/foo/bar/baz", p1)
		r.Insert("/foo/something/baz", p2)

		r.Delete("/foo/bar/baz")
		_, ok := r.Find("/foo/something/baz")
		Expect(ok).To(BeTrue())
	})

	It("Delete does not prune nodes with pools", func() {
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		r.Insert("/foo/bar/baz", p1)
		r.Insert("/foo/bar", p2)

		r.Delete("/foo/bar/baz")
		_, ok := r.Find("/foo/bar")
		Expect(ok).To(BeTrue())
	})

	It("Returns the number of pools after deleting one", func() {
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		r.Insert("/foo/bar/baz", p1)
		r.Insert("/foo/bar", p2)

		r.Delete("/foo/bar/baz")
		Expect(r.PoolCount()).To(Equal(1))
	})

	It("Returns the number of pools", func() {
		Expect(r.PoolCount()).To(Equal(0))

		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		r.Insert("/foo/bar/baz", p1)
		r.Insert("/foo/bar", p2)

		Expect(r.PoolCount()).To(Equal(2))
	})

	It("Prunes dead leaves", func() {
		segments := make([]string, 0)
		count := 0
		f := func(r *Trie) {
			segments = append(segments, r.Segment)
			count += 1
		}

		e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", nil, -1)
		e2 := route.NewEndpoint("", "192.168.1.1", 4321, "", nil, -1)
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		p3 := route.NewPool(42)
		p4 := route.NewPool(42)
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

	It("snips", func() {
		e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", nil, -1)
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
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

	It("EndpointCount returns the number of endpoints", func() {
		Expect(r.EndpointCount()).To(Equal(0))

		e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", nil, -1)
		e2 := route.NewEndpoint("", "192.168.1.1", 4321, "", nil, -1)
		p := route.NewPool(42)
		p.Put(e1)
		p.Put(e2)
		r.Insert("/foo/bar", p)

		Expect(r.EndpointCount()).To(Equal(2))
	})

	It("EndpointCount uniques the endpoints", func() {
		e1 := route.NewEndpoint("", "192.168.1.1", 1234, "", nil, -1)
		e2 := route.NewEndpoint("", "192.168.1.1", 1234, "", nil, -1)
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		p1.Put(e1)
		p2.Put(e2)
		r.Insert("/foo", p1)
		r.Insert("/foo/bar", p2)

		Expect(r.EndpointCount()).To(Equal(1))
	})

	It("applies a function to each node with a pool", func() {
		p1 := route.NewPool(42)
		p2 := route.NewPool(42)
		r.Insert("/foo", p1)
		r.Insert("/foo/bar/baz", p2)

		pools := make([]*route.Pool, 0)
		r.EachNodeWithPool(func(node *Trie) {
			pools = append(pools, node.Pool)
		})

		Expect(pools).To(HaveLen(2))
		Expect(pools).To(ContainElement(p1))
		Expect(pools).To(ContainElement(p2))
	})
})
