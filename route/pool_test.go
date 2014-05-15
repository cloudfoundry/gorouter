package route_test

import (
	. "github.com/cloudfoundry/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Route", func() {
	Context("Add", func() {
		It("adds endpoints", func() {
			pool := NewPool()
			endpoint := &Endpoint{}

			pool.Add(endpoint)
			foundEndpoint, found := pool.Sample()
			Ω(found).To(BeTrue())
			Ω(foundEndpoint).To(Equal(endpoint))
		})

		It("handles duplicate endpoints", func() {
			pool := NewPool()

			endpoint := &Endpoint{}

			pool.Add(endpoint)
			pool.Add(endpoint)

			foundEndpoint, found := pool.Sample()
			Ω(found).To(BeTrue())
			Ω(foundEndpoint).To(Equal(endpoint))

			pool.Remove(endpoint)

			_, found = pool.Sample()
			Ω(found).To(BeFalse())
		})

		It("handles equivalent (duplicate) endpoints", func() {
			pool := NewPool()

			endpoint1 := &Endpoint{Host: "1.2.3.4", Port: 5678}
			endpoint2 := &Endpoint{Host: "1.2.3.4", Port: 5678}

			pool.Add(endpoint1)
			pool.Add(endpoint2)

			_, found := pool.Sample()
			Ω(found).To(BeTrue())

			pool.Remove(endpoint1)

			_, found = pool.Sample()
			Ω(found).To(BeFalse())
		})
	})
	Context("Remove", func() {
		It("removes endpoints", func() {
			pool := NewPool()

			endpoint := &Endpoint{}

			pool.Add(endpoint)

			foundEndpoint, found := pool.Sample()
			Ω(found).To(BeTrue())
			Ω(foundEndpoint).To(Equal(endpoint))

			pool.Remove(endpoint)

			_, found = pool.Sample()
			Ω(found).To(BeFalse())
		})

	})
	Context("IsEmpty", func() {
		It("starts empty", func() {
			Ω(NewPool().IsEmpty()).To(BeTrue())
		})

		It("empty after removing everything", func() {
			pool := NewPool()

			endpoint := &Endpoint{}

			pool.Add(endpoint)

			Ω(pool.IsEmpty()).To(BeFalse())

			pool.Remove(endpoint)

			Ω(pool.IsEmpty()).To(BeTrue())
		})
	})

	It("finds by private instance id", func() {
		pool := NewPool()

		endpointFoo := &Endpoint{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"}
		endpointBar := &Endpoint{Host: "5.6.7.8", Port: 5678, PrivateInstanceId: "bar"}

		pool.Add(endpointFoo)
		pool.Add(endpointBar)

		foundEndpoint, found := pool.FindByPrivateInstanceId("foo")
		Ω(found).To(BeTrue())
		Ω(foundEndpoint).To(Equal(endpointFoo))

		foundEndpoint, found = pool.FindByPrivateInstanceId("bar")
		Ω(found).To(BeTrue())
		Ω(foundEndpoint).To(Equal(endpointBar))

		_, found = pool.FindByPrivateInstanceId("quux")
		Ω(found).To(BeFalse())
	})

	It("Sample is randomish", func() {
		pool := NewPool()

		endpoint1 := &Endpoint{Host: "1.2.3.4", Port: 5678}
		endpoint2 := &Endpoint{Host: "5.6.7.8", Port: 1234}

		pool.Add(endpoint1)
		pool.Add(endpoint2)

		var occurrences1, occurrences2 int

		for i := 0; i < 200; i += 1 {
			foundEndpoint, _ := pool.Sample()
			if foundEndpoint == endpoint1 {
				occurrences1 += 1
			} else {
				occurrences2 += 1
			}
		}

		Ω(occurrences1).ToNot(BeZero())
		Ω(occurrences2).ToNot(BeZero())

		// they should be arbitrarily close
		Ω(occurrences1 - occurrences2).To(BeNumerically("~", 0, 50))
	})

	It("marshals json", func() {
		pool := NewPool()

		pool.Add(&Endpoint{Host: "1.2.3.4", Port: 5678})

		json, err := pool.MarshalJSON()
		Ω(err).ToNot(HaveOccurred())

		Ω(string(json)).To(Equal(`["1.2.3.4:5678"]`))
	})
})
