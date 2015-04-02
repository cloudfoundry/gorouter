package workpool_test

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/cloudfoundry/gunk/workpool"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Workpool", func() {
	var pool *WorkPool
	var poolSize int
	var pendingSize int
	var around func(work func())

	BeforeEach(func() {
		poolSize = 2
		around = DefaultAround
	})

	JustBeforeEach(func() {
		pool = New(poolSize, pendingSize, AroundWorkFunc(around))
	})

	AfterEach(func() {
		pool.Stop()
	})

	Describe("scheduling work", func() {
		Context("no pending work allowed", func() {
			BeforeEach(func() {
				pendingSize = 0
			})

			Context("when passed one work", func() {
				It("should run the passed in function", func() {
					called := make(chan bool)

					pool.Submit(func() {
						called <- true
					})

					Eventually(called, 0.1, 0.01).Should(Receive())
				})
			})

			Context("when passed many work", func() {
				var (
					startTime time.Time
					runTimes  chan time.Duration
					sleepTime time.Duration
					work      func()
				)

				BeforeEach(func() {
					startTime = time.Now()
					runTimes = make(chan time.Duration, 10)
					sleepTime = time.Duration(0.01 * float64(time.Second))

					work = func() {
						time.Sleep(sleepTime)
						runTimes <- time.Since(startTime)
					}
				})

				Context("when passed poolSize work", func() {
					JustBeforeEach(func() {
						pool.Submit(work)
						pool.Submit(work)
					})

					It("should run the functions concurrently", func() {
						Eventually(runTimes, 0.1, 0.01).Should(HaveLen(2))
						Ω(<-runTimes).Should(BeNumerically("<=", sleepTime+sleepTime/2))
						Ω(<-runTimes).Should(BeNumerically("<=", sleepTime+sleepTime/2))
					})
				})

				Context("when passed more than poolSize work", func() {
					JustBeforeEach(func() {
						pool.Submit(work)
						pool.Submit(work)
						pool.Submit(work)
					})

					It("should run all the functions, but at most poolSize at a time", func() {
						Eventually(runTimes, 0.1, 0.01).Should(HaveLen(3))

						//first batch
						Ω(<-runTimes).Should(BeNumerically("<=", sleepTime+sleepTime/2))
						Ω(<-runTimes).Should(BeNumerically("<=", sleepTime+sleepTime/2))

						//second batch
						Ω(<-runTimes).Should(BeNumerically(">=", sleepTime*2))
					})
				})
			})
		})

		Context("pending work allowed", func() {
			BeforeEach(func() {
				pendingSize = 1
			})

			Context("when passed more than poolSize work", func() {
				It("should not block the caller", func() {
					barrier := make(chan struct{})
					wg := sync.WaitGroup{}

					work := func() {
						wg.Done()
						<-barrier
					}

					defer close(barrier)

					wg.Add(2)
					pool.Submit(work)
					pool.Submit(work)

					wg.Wait()

					var count int32
					go func() {
						pool.Submit(func() {
							Ω(atomic.CompareAndSwapInt32(&count, 1, 2)).Should(BeTrue())
						})
						Ω(atomic.CompareAndSwapInt32(&count, 0, 1)).Should(BeTrue())
					}()

					Eventually(func() int32 { return atomic.LoadInt32(&count) }).Should(Equal(int32(1)))
					barrier <- struct{}{}

					Eventually(func() int32 { return atomic.LoadInt32(&count) }).Should(Equal(int32(2)))
				})
			})
		})

		Context("when stopped", func() {
			var numGoroutines int

			JustBeforeEach(func() {
				numGoroutines = runtime.NumGoroutine()
				pool.Stop()
			})

			It("should never perform the work", func() {
				called := make(chan bool, 1)

				pool.Submit(func() {
					called <- true
				})

				Consistently(called).ShouldNot(Receive())
			})

			It("should stop the workers", func() {
				Eventually(runtime.NumGoroutine, 0.1, 0.01).Should(Equal(numGoroutines-2), "Should have reduced number of go routines by pool size")
			})
		})

		Context("when around is provided", func() {
			var count int32

			BeforeEach(func() {
				count = 0
				around = func(work func()) {
					atomic.AddInt32(&count, 1)
				}
			})
			It("is called for each work", func() {
				work := func() {}
				pool.Submit(work)
				pool.Submit(work)

				Eventually(func() int32 {
					return atomic.LoadInt32(&count)
				}).Should(Equal(int32(2)))
			})
		})
	})
})
