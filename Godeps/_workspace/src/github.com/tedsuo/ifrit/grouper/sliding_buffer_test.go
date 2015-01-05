package grouper

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const capacity = 3

var _ = Describe("Sliding Buffer", func() {
	var buffer slidingBuffer
	BeforeEach(func() {
		buffer = newSlidingBuffer(capacity)
	})

	Context("when the number of appends exceeds capacity", func() {
		BeforeEach(func() {
			for i := 0; i < capacity*2; i++ {
				buffer.Append(i)
			}
		})

		It("adds to the buffer, up to the capacity", func() {
			Ω(buffer.Length()).Should(Equal(capacity))
		})

		It("Range returns the most recently added items", func() {
			expectedIndex := capacity
			buffer.Range(func(item interface{}) {
				index := item.(int)
				Ω(index).Should(Equal(expectedIndex))
				expectedIndex++
			})
		})
	})
})
