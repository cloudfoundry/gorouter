package grouper_test

import (
	"os"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/fake_runner"
	"github.com/tedsuo/ifrit/grouper"
)

var _ = Describe("dynamicGroup", func() {
	var (
		client      grouper.DynamicClient
		pool        grouper.DynamicGroup
		poolProcess ifrit.Process

		childRunner1 *fake_runner.TestRunner
		childRunner2 *fake_runner.TestRunner
		childRunner3 *fake_runner.TestRunner
	)

	BeforeEach(func() {
		childRunner1 = fake_runner.NewTestRunner()
		childRunner2 = fake_runner.NewTestRunner()
		childRunner3 = fake_runner.NewTestRunner()
	})
	AfterEach(func() {
		childRunner1.EnsureExit()
		childRunner2.EnsureExit()
		childRunner3.EnsureExit()
	})

	Describe("Get", func() {
		var member1, member2, member3 grouper.Member

		BeforeEach(func() {
			member1 = grouper.Member{"child1", childRunner1}
			member2 = grouper.Member{"child2", childRunner2}
			member3 = grouper.Member{"child3", childRunner3}

			pool = grouper.NewDynamic(nil, 3, 2)
			client = pool.Client()
			poolProcess = ifrit.Envoke(pool)

			insert := client.Inserter()
			Eventually(insert).Should(BeSent(member1))
			Eventually(insert).Should(BeSent(member2))
			Eventually(insert).Should(BeSent(member3))
		})

		It("returns a process when the member is present", func() {
			signal1 := childRunner1.WaitForCall()
			p, ok := client.Get("child1")
			Ω(ok).Should(BeTrue())
			p.Signal(syscall.SIGUSR2)
			Eventually(signal1).Should(Receive(Equal(syscall.SIGUSR2)))
		})

		It("returns false when the member is not present", func() {
			_, ok := client.Get("blah")
			Ω(ok).Should(BeFalse())
		})
	})

	Describe("Insert", func() {
		var member1, member2, member3 grouper.Member

		BeforeEach(func() {
			member1 = grouper.Member{"child1", childRunner1}
			member2 = grouper.Member{"child2", childRunner2}
			member3 = grouper.Member{"child3", childRunner3}

			pool = grouper.NewDynamic(nil, 3, 2)
			client = pool.Client()
			poolProcess = ifrit.Envoke(pool)

			insert := client.Inserter()
			Eventually(insert).Should(BeSent(member1))
			Eventually(insert).Should(BeSent(member2))
			Eventually(insert).Should(BeSent(member3))
		})

		AfterEach(func() {
			poolProcess.Signal(os.Kill)
			Eventually(poolProcess.Wait()).Should(Receive())
		})

		It("announces the events as processes move through their lifecycle", func() {
			entrance1, entrance2, entrance3 := grouper.EntranceEvent{}, grouper.EntranceEvent{}, grouper.EntranceEvent{}
			exit1, exit2, exit3 := grouper.ExitEvent{}, grouper.ExitEvent{}, grouper.ExitEvent{}

			entrances := client.EntranceListener()
			exits := client.ExitListener()

			childRunner2.TriggerReady()
			Eventually(entrances).Should(Receive(&entrance2))
			Ω(entrance2.Member).Should(Equal(member2))

			childRunner1.TriggerReady()
			Eventually(entrances).Should(Receive(&entrance1))
			Ω(entrance1.Member).Should(Equal(member1))

			childRunner3.TriggerReady()
			Eventually(entrances).Should(Receive(&entrance3))
			Ω(entrance3.Member).Should(Equal(member3))

			childRunner2.TriggerExit(nil)
			Eventually(exits).Should(Receive(&exit2))
			Ω(exit2.Member).Should(Equal(member2))

			childRunner1.TriggerExit(nil)
			Eventually(exits).Should(Receive(&exit1))
			Ω(exit1.Member).Should(Equal(member1))

			childRunner3.TriggerExit(nil)
			Eventually(exits).Should(Receive(&exit3))
			Ω(exit3.Member).Should(Equal(member3))
		})

		It("announces the most recent events that have already occured, up to the buffer size", func() {
			entrance2, entrance3 := grouper.EntranceEvent{}, grouper.EntranceEvent{}
			exit2, exit3 := grouper.ExitEvent{}, grouper.ExitEvent{}

			childRunner1.TriggerReady()
			childRunner2.TriggerReady()
			childRunner3.TriggerReady()
			time.Sleep(time.Millisecond)

			entrances := client.EntranceListener()

			Eventually(entrances).Should(Receive(&entrance2))
			Ω(entrance2.Member).Should(Equal(member2))

			Eventually(entrances).Should(Receive(&entrance3))
			Ω(entrance3.Member).Should(Equal(member3))

			Consistently(entrances).ShouldNot(Receive())

			childRunner1.TriggerExit(nil)
			childRunner2.TriggerExit(nil)
			childRunner3.TriggerExit(nil)
			time.Sleep(time.Millisecond)

			exits := client.ExitListener()
			Eventually(exits).Should(Receive(&exit2))
			Ω(exit2.Member).Should(Equal(member2))

			Eventually(exits).Should(Receive(&exit3))
			Ω(exit3.Member).Should(Equal(member3))

			Consistently(exits).ShouldNot(Receive())
		})
	})
})
