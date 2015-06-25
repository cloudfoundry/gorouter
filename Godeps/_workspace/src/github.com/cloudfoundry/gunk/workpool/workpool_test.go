package workpool_test

import (
	"github.com/cloudfoundry/gunk/workpool"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("WorkPool", func() {
	var pool *workpool.WorkPool

	AfterEach(func() {
		if pool != nil {
			pool.Stop()
		}
	})

	Context("when max workers is non-positive", func() {
		It("errors", func() {
			_, err := workpool.NewWorkPool(0)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when max workers is positive", func() {
		var maxWorkers int
		var calledChan chan int
		var unblockChan chan struct{}
		var work func(int) func()

		BeforeEach(func() {
			maxWorkers = 2
			calledChan = make(chan int)
			unblockChan = make(chan struct{})
			work = func(i int) func() {
				return func() {
					calledChan := calledChan
					unblockChan := unblockChan
					calledChan <- i
					<-unblockChan
				}
			}

			var err error
			pool, err = workpool.NewWorkPool(maxWorkers)
			Expect(err).NotTo(HaveOccurred())
		})

		Describe("Submit", func() {
			Context("when submitting less work than the max number of workers", func() {
				It("should run the passed-in work", func() {
					for i := 0; i < maxWorkers-1; i++ {
						pool.Submit(work(i))
					}

					for i := 0; i < maxWorkers-1; i++ {
						Eventually(calledChan).Should(Receive(Equal(i)))
					}
				})
			})

			Context("when submitting work equal to the number of workers", func() {
				It("should run the passed-in work concurrently", func() {
					for i := 0; i < maxWorkers; i++ {
						pool.Submit(work(i))
					}

					for i := 0; i < maxWorkers; i++ {
						Eventually(calledChan).Should(Receive(Equal(i)))
					}
				})
			})

			Context("when submitting more work than the max number of workers", func() {
				It("should run the passed-in work concurrently up to the max number of workers at a time", func() {
					for i := 0; i < maxWorkers+1; i++ {
						pool.Submit(work(i))
					}

					for i := 0; i < maxWorkers; i++ {
						Eventually(calledChan).Should(Receive(Equal(i)))
					}
					Consistently(calledChan).ShouldNot(Receive())

					unblockChan <- struct{}{}

					Eventually(calledChan).Should(Receive(Equal(maxWorkers)))
				})
			})
		})

		Describe("Stop", func() {
			It("does not start any newly-submitted work", func() {
				pool.Stop()
				pool.Submit(work(0))

				Consistently(calledChan).ShouldNot(Receive())
			})

			It("does not start any pending work", func() {
				for i := 0; i < maxWorkers+1; i++ {
					pool.Submit(work(i))
				}

				for i := 0; i < maxWorkers; i++ {
					Eventually(calledChan).Should(Receive(Equal(i)))
				}
				Consistently(calledChan).ShouldNot(Receive())

				pool.Stop()
				unblockChan <- struct{}{}

				Consistently(calledChan).ShouldNot(Receive())
			})
		})
	})
})
