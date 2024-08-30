package health_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "code.cloudfoundry.org/gorouter/common/health"
)

var _ = Describe("Health", func() {
	var (
		h *Health
	)

	BeforeEach(func() {
		h = &Health{}
	})

	Context("when is healthy", func() {
		It("reports healhty", func() {
			h.SetHealth(Healthy)

			Expect(h.Health()).To(Equal(Healthy))
		})

		It("does not degrade", func() {
			called := false
			h.OnDegrade = func() {
				called = true
			}

			h.SetHealth(Healthy)
			Expect(called).To(BeFalse(), "OnDegrade was called")
		})

		Context("set degraded", func() {
			BeforeEach(func() {
				h.SetHealth(Healthy)
			})

			It("updates the status", func() {
				h.SetHealth(Degraded)

				Expect(h.Health()).To(Equal(Degraded))
			})

			It("calls h.onDegrade callback", func() {
				called := false
				h.OnDegrade = func() {
					called = true
				}

				h.SetHealth(Degraded)

				Expect(called).To(BeTrue(), "OnDegrade wasn't called")
			})
		})
	})

	Context("when is degraded", func() {
		calledN := 0

		BeforeEach(func() {
			calledN = 0
			h.OnDegrade = func() {
				calledN++
			}

			h.SetHealth(Degraded)
		})

		Context("set degraded", func() {
			It("does not call h.onDegrade callback", func() {
				h.SetHealth(Degraded)

				Expect(calledN).To(Equal(1), "OnDegrade was called multiple times")
			})
		})
	})
})
