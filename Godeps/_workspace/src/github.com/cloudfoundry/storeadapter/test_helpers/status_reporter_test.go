package test_helpers_test

import (
	. "github.com/cloudfoundry/storeadapter/test_helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("StatusReporter", func() {
	var statusReporter *StatusReporter
	var status chan bool

	BeforeEach(func() {
		status = make(chan bool)

		statusReporter = NewStatusReporter(status)
	})

	Describe("Reporting", func() {
		Context("when no status has been reported", func() {
			It("returns false", func() {
				Ω(statusReporter.Reporting()).Should(BeFalse())
			})
		})

		Context("when a value has been reported", func() {
			BeforeEach(func() {
				status <- false
			})

			It("eventually returns true", func() {
				Eventually(statusReporter.Reporting).Should(BeTrue())
			})

			Context("and then the status channel is closed", func() {
				BeforeEach(func() {
					close(status)
				})

				It("eventually returns false", func() {
					Eventually(statusReporter.Reporting).Should(BeFalse())
				})
			})
		})
	})

	Describe("Locked", func() {
		Context("when true is reported", func() {
			BeforeEach(func() {
				status <- true
			})

			It("eventually returns true", func() {
				Eventually(statusReporter.Locked).Should(BeTrue())
			})

			Context("and then false is reported", func() {
				BeforeEach(func() {
					status <- false
				})

				It("eventually returns false again", func() {
					Eventually(statusReporter.Locked).Should(BeFalse())
				})
			})
		})
	})
})
