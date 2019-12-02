package handlers_test

import (
	"encoding/hex"

	"code.cloudfoundry.org/gorouter/handlers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("W3CTraceparent", func() {
	Context("when given a valid traceparent header", func() {
		var (
			sampleHeader string

			expectedTraceID  []byte
			expectedParentID []byte
		)

		BeforeEach(func() {
			expectedTraceID, _ = hex.DecodeString(
				"4bf92f3577b34da6a3ce929d0e0e4736",
			)
			expectedParentID, _ = hex.DecodeString(
				"00f067aa0ba902b7",
			)
		})

		Context("when the request has been sampled", func() {
			BeforeEach(func() {
				sampleHeader = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
			})

			It("should parse correctly", func() {
				parsed := handlers.ParseW3CTraceparent(sampleHeader)

				Expect(parsed).NotTo(BeNil())

				Expect(parsed.Version).To(Equal(handlers.W3CTraceparentVersion))
				Expect(parsed.Flags).To(Equal(handlers.W3CTraceparentSampled))

				Expect(parsed.TraceID).To(Equal(expectedTraceID))
				Expect(parsed.ParentID).To(Equal(expectedParentID))

				Expect(parsed.String()).To(Equal(sampleHeader))
			})

			It("should generate a new header correctly", func() {
				parsed := handlers.ParseW3CTraceparent(sampleHeader)

				Expect(parsed).NotTo(BeNil())
				Expect(parsed.String()).To(Equal(sampleHeader))

				next, err := parsed.Next()

				Expect(err).NotTo(HaveOccurred())

				Expect(parsed.TraceID).To(
					Equal(next.TraceID), "the trace IDs should match",
				)

				Expect(parsed.ParentID).NotTo(
					Equal(next.ParentID), "the parent IDs should not match",
				)

				Expect(next.Version).To(Equal(handlers.W3CTraceparentVersion))
				Expect(next.Flags).To(Equal(parsed.Flags))
			})
		})

		Context("when the request has not been sampled", func() {
			BeforeEach(func() {
				sampleHeader = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"
			})

			It("should parse correctly", func() {

				parsed := handlers.ParseW3CTraceparent(sampleHeader)

				Expect(parsed).NotTo(BeNil())

				Expect(parsed.Version).To(Equal(handlers.W3CTraceparentVersion))
				Expect(parsed.Flags).To(Equal(handlers.W3CTraceparentNotSampled))

				Expect(parsed.TraceID).To(Equal(expectedTraceID))
				Expect(parsed.ParentID).To(Equal(expectedParentID))

				Expect(parsed.String()).To(Equal(sampleHeader))
			})

			It("should generate a new header correctly", func() {
				parsed := handlers.ParseW3CTraceparent(sampleHeader)

				Expect(parsed).NotTo(BeNil())
				Expect(parsed.String()).To(Equal(sampleHeader))

				next, err := parsed.Next()

				Expect(err).NotTo(HaveOccurred())

				Expect(parsed.TraceID).To(
					Equal(next.TraceID), "the trace IDs should match",
				)

				Expect(parsed.ParentID).NotTo(
					Equal(next.ParentID), "the parent IDs should not match",
				)

				Expect(next.Version).To(Equal(handlers.W3CTraceparentVersion))
				Expect(next.Flags).To(Equal(parsed.Flags))
			})
		})

		Context("when the request has some non-standard flags", func() {
			BeforeEach(func() {
				sampleHeader = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-99"
			})

			It("should parse correctly", func() {

				parsed := handlers.ParseW3CTraceparent(sampleHeader)

				Expect(parsed).NotTo(BeNil())

				Expect(parsed.Version).To(Equal(handlers.W3CTraceparentVersion))
				Expect(parsed.Flags).To(Equal(uint8(153)), "Hex -> Dec")

				Expect(parsed.TraceID).To(Equal(expectedTraceID))
				Expect(parsed.ParentID).To(Equal(expectedParentID))

				Expect(parsed.String()).To(Equal(sampleHeader))
			})

			It("should generate a new header correctly", func() {
				parsed := handlers.ParseW3CTraceparent(sampleHeader)

				Expect(parsed).NotTo(BeNil())
				Expect(parsed.String()).To(Equal(sampleHeader))

				next, err := parsed.Next()

				Expect(err).NotTo(HaveOccurred())

				Expect(parsed.TraceID).To(
					Equal(next.TraceID), "the trace IDs should match",
				)

				Expect(parsed.ParentID).NotTo(
					Equal(next.ParentID), "the parent IDs should not match",
				)

				Expect(next.Version).To(Equal(handlers.W3CTraceparentVersion))
				Expect(next.Flags).To(Equal(uint8(153)), "Hex -> Dec")
			})
		})
	})
})
