package utils_test

import (
	"code.cloudfoundry.org/gorouter/proxy/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("CollectHeadersToLog", func() {
	Context("when there are no headers to be logged", func() {
		It("returns no headers", func() {
			Expect(utils.CollectHeadersToLog()).To(HaveLen(0))
		})
	})

	Context("when there are some headers to be logged", func() {
		It("returns the headers in order", func() {
			headersToLog := utils.CollectHeadersToLog(
				[]string{"X-Forwarded-For", "Host", "Content-Length"},
			)

			Expect(headersToLog).To(HaveLen(3))
			Expect(headersToLog[0]).To(Equal("X-Forwarded-For"))
			Expect(headersToLog[1]).To(Equal("Host"))
			Expect(headersToLog[2]).To(Equal("Content-Length"))
		})
	})

	Context("when there are multiple groups of headers to be logged", func() {
		Context("when there are no duplicates", func() {
			It("returns the headers in order", func() {
				headersToLog := utils.CollectHeadersToLog(
					[]string{"X-Forwarded-For", "Host", "Content-Length"},
					[]string{"X-Forwarded-Proto", "Content-Type"},
				)

				Expect(headersToLog).To(HaveLen(5))
				Expect(headersToLog[0]).To(Equal("X-Forwarded-For"))
				Expect(headersToLog[1]).To(Equal("Host"))
				Expect(headersToLog[2]).To(Equal("Content-Length"))
				Expect(headersToLog[3]).To(Equal("X-Forwarded-Proto"))
				Expect(headersToLog[4]).To(Equal("Content-Type"))
			})
		})
		Context("when there are duplicates", func() {
			It("returns the headers in order", func() {
				headersToLog := utils.CollectHeadersToLog(
					[]string{"X-Forwarded-For", "Host", "Content-Length"},
					[]string{"X-Forwarded-Proto", "Content-Type", "Host"},
				)

				Expect(headersToLog).To(HaveLen(5))
				Expect(headersToLog[0]).To(Equal("X-Forwarded-For"))
				Expect(headersToLog[1]).To(Equal("Host"))
				Expect(headersToLog[2]).To(Equal("Content-Length"))
				Expect(headersToLog[3]).To(Equal("X-Forwarded-Proto"))
				Expect(headersToLog[4]).To(Equal("Content-Type"))
			})
		})
	})
})
