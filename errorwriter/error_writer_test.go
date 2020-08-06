package errorwriter_test

import (
	_ "html/template"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"

	. "code.cloudfoundry.org/gorouter/errorwriter"
	loggerfakes "code.cloudfoundry.org/gorouter/logger/fakes"
)

var _ = Describe("Plaintext ErrorWriter", func() {
	var (
		errorWriter ErrorWriter
		recorder    *httptest.ResponseRecorder

		log *loggerfakes.FakeLogger
	)

	BeforeEach(func() {
		errorWriter = NewPlaintextErrorWriter()
		recorder = httptest.NewRecorder()
		recorder.Header().Set("Connection", "dummy")

		log = new(loggerfakes.FakeLogger)
	})

	Context("when the response code is a success", func() {
		BeforeEach(func() {
			errorWriter.WriteError(recorder, http.StatusOK, "hi", log)
		})

		It("should write the status code", func() {
			Expect(recorder.Result().StatusCode).To(Equal(http.StatusOK))
		})

		It("should write the message", func() {
			Eventually(BufferReader(recorder.Result().Body)).Should(Say("hi"))
		})

		It("should log the message", func() {
			Expect(log.InfoCallCount()).NotTo(Equal(0))
			message, _ := log.InfoArgsForCall(0)
			Expect(message).To(Equal("status"))
		})

		It("should keep the connection header", func() {
			Expect(recorder.Result().Header.Get("Connection")).To(Equal("dummy"))
		})
	})

	Context("when the response code is not a success", func() {
		BeforeEach(func() {
			errorWriter.WriteError(recorder, http.StatusBadRequest, "bad", log)
		})

		It("should write the status code", func() {
			Expect(recorder.Result().StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should write the message", func() {
			Eventually(BufferReader(recorder.Result().Body)).Should(Say("bad"))
		})

		It("should log the message", func() {
			Expect(log.InfoCallCount()).NotTo(Equal(0))
			message, _ := log.InfoArgsForCall(0)
			Expect(message).To(Equal("status"))
		})

		It("should delete the connection header", func() {
			Expect(recorder.Result().Header.Get("Connection")).To(Equal(""))
		})
	})
})

var _ = Describe("HTML ErrorWriter", func() {
	var (
		errorWriter ErrorWriter
		recorder    *httptest.ResponseRecorder

		log *loggerfakes.FakeLogger
	)

	BeforeEach(func() {
		recorder = httptest.NewRecorder()
		recorder.Header().Set("Connection", "dummy")

		log = new(loggerfakes.FakeLogger)
	})

	Context("when the template has invalid syntax", func() {
		It("should return constructor error", func() {
			var err error
			_, err = NewHTMLErrorWriter("{{")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when the template errors", func() {
		Context("when the response is a success", func() {
			BeforeEach(func() {
				var err error
				errorWriter, err = NewHTMLErrorWriter(`{{template "notexists"}}`)
				Expect(err).NotTo(HaveOccurred())

				errorWriter.WriteError(recorder, http.StatusOK, "hi", log)
			})

			It("should write the status code", func() {
				Expect(recorder.Result().StatusCode).To(Equal(http.StatusOK))
			})

			It("should write the message as text", func() {
				Eventually(BufferReader(recorder.Result().Body)).Should(Say("200 OK: hi"))
			})

			It("should log the message", func() {
				Expect(log.InfoCallCount()).NotTo(Equal(0))
				message, _ := log.InfoArgsForCall(0)
				Expect(message).To(Equal("status"))
			})

			It("should keep the connection header", func() {
				Expect(recorder.Result().Header.Get("Connection")).To(Equal("dummy"))
			})
		})

		Context("when the response is not a success", func() {
			BeforeEach(func() {
				var err error
				errorWriter, err = NewHTMLErrorWriter(`{{template "notexists"}}`)
				Expect(err).NotTo(HaveOccurred())

				errorWriter.WriteError(recorder, http.StatusBadRequest, "bad", log)
			})

			It("should write the status code", func() {
				Expect(recorder.Result().StatusCode).To(Equal(http.StatusBadRequest))
			})

			It("should write the message as text", func() {
				Eventually(BufferReader(recorder.Result().Body)).Should(Say("400 Bad Request: bad"))
			})

			It("should delete the connection header", func() {
				Expect(recorder.Result().Header.Get("Connection")).To(Equal(""))
			})
		})
	})

	Context("when the template renders", func() {
		var (
			err error
		)

		Context("when the response is a success", func() {
			BeforeEach(func() {
				errorWriter, err = NewHTMLErrorWriter(`success`)
				Expect(err).NotTo(HaveOccurred())

				errorWriter.WriteError(recorder, http.StatusOK, "hi", log)
			})

			It("should not return an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("should write the status code", func() {
				Expect(recorder.Result().StatusCode).To(Equal(http.StatusOK))
			})

			XIt("should write the message as text", func() {
				Eventually(BufferReader(recorder.Result().Body)).Should(Say("200 OK: hi"))
			})

			It("should keep the connection header", func() {
				Expect(recorder.Result().Header.Get("Connection")).To(Equal("dummy"))
			})
		})

		Context("when the response is not a success", func() {
			BeforeEach(func() {
				errorWriter, err = NewHTMLErrorWriter(`failure`)
				Expect(err).NotTo(HaveOccurred())

				errorWriter.WriteError(recorder, http.StatusBadRequest, "bad", log)
			})

			It("should not return an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("should write the status code", func() {
				Expect(recorder.Result().StatusCode).To(Equal(http.StatusBadRequest))
			})

			XIt("should write the message as text", func() {
				Eventually(BufferReader(recorder.Result().Body)).Should(Say("400 Bad Request: bad"))
			})

			It("should delete the connection header", func() {
				Expect(recorder.Result().Header.Get("Connection")).To(Equal(""))
			})
		})
	})
})
