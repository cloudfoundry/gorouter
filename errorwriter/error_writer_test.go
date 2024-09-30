package errorwriter_test

import (
	_ "html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"

	. "code.cloudfoundry.org/gorouter/errorwriter"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("Plaintext ErrorWriter", func() {
	var (
		errorWriter ErrorWriter
		recorder    *httptest.ResponseRecorder

		logger *test_util.TestLogger
	)

	BeforeEach(func() {
		errorWriter = NewPlaintextErrorWriter()
		recorder = httptest.NewRecorder()
		recorder.Header().Set("Connection", "dummy")
		logger = test_util.NewTestLogger("test")
	})

	Context("when the response code is a success", func() {
		BeforeEach(func() {
			errorWriter.WriteError(recorder, http.StatusOK, "hi", logger.Logger)
		})

		It("should write the status code", func() {
			Expect(recorder.Result().StatusCode).To(Equal(http.StatusOK))
		})

		It("should write the message", func() {
			Eventually(BufferReader(recorder.Result().Body)).Should(Say("hi"))
		})

		It("should log the message", func() {
			Expect(logger.TestSink.Lines()).To(HaveLen(1))
			Expect(logger.TestSink.Lines()[0]).To(MatchRegexp(
				`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"status".+}`,
			))
		})

		It("should keep the Connection header", func() {
			Expect(recorder.Result().Header.Get("Connection")).To(Equal("dummy"))
		})

		It("should set the Content-Type header", func() {
			Expect(recorder.Result().Header.Get("Content-Type")).To(Equal("text/plain; charset=utf-8"))
		})

		It("should set the X-Content-Type-Options header", func() {
			Expect(recorder.Result().Header.Get("X-Content-Type-Options")).To(Equal("nosniff"))
		})
	})

	Context("when the response code is not a success", func() {
		BeforeEach(func() {
			errorWriter.WriteError(recorder, http.StatusBadRequest, "bad", logger.Logger)
		})

		It("should write the status code", func() {
			Expect(recorder.Result().StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should write the message", func() {
			Eventually(BufferReader(recorder.Result().Body)).Should(Say("bad"))
		})

		It("should log the message", func() {
			Expect(logger.TestSink.Lines()[0]).To(MatchRegexp(
				`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"status".+}`,
			))
		})

		It("should delete the Connection header", func() {
			Expect(recorder.Result().Header.Get("Connection")).To(Equal(""))
		})
	})
})

var _ = Describe("HTML ErrorWriter", func() {
	var (
		tmpFile *os.File

		errorWriter ErrorWriter
		recorder    *httptest.ResponseRecorder

		logger   *slog.Logger
		testSink *test_util.TestSink
	)

	BeforeEach(func() {
		var err error
		tmpFile, err = os.CreateTemp(os.TempDir(), "html-err-tpl")
		Expect(err).NotTo(HaveOccurred())

		recorder = httptest.NewRecorder()
		recorder.Header().Set("Connection", "dummy")
		testSink = &test_util.TestSink{Buffer: NewBuffer()}
		logger = log.CreateLogger()
		testSink = &test_util.TestSink{Buffer: NewBuffer()}
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		log.SetLoggingLevel("Debug")
	})

	AfterEach(func() {
		os.Remove(tmpFile.Name())
	})

	Context("when the template file does not exist", func() {
		It("should return constructor error", func() {
			var err error
			_, err = NewHTMLErrorWriterFromFile("/path/to/non/file")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when the template has invalid syntax", func() {
		BeforeEach(func() {
			_, err := tmpFile.Write([]byte("{{"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return constructor error", func() {
			var err error
			_, err = NewHTMLErrorWriterFromFile(tmpFile.Name())
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when the template errors", func() {
		BeforeEach(func() {
			_, err := tmpFile.Write([]byte(`{{template "notexists"}}`))
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when the response is a success", func() {
			BeforeEach(func() {
				var err error
				errorWriter, err = NewHTMLErrorWriterFromFile(tmpFile.Name())
				Expect(err).NotTo(HaveOccurred())

				errorWriter.WriteError(recorder, http.StatusOK, "hi", logger)
			})

			It("should write the status code", func() {
				Expect(recorder.Result().StatusCode).To(Equal(http.StatusOK))
			})

			It("should write the message as text", func() {
				Eventually(BufferReader(recorder.Result().Body)).Should(Say("200 OK: hi"))
			})

			It("should log the message", func() {
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"status".+}`,
				))
			})

			It("should keep the Connection header", func() {
				Expect(recorder.Result().Header.Get("Connection")).To(Equal("dummy"))
			})

			It("should set the Content-Type header", func() {
				Expect(recorder.Result().Header.Get("Content-Type")).To(Equal("text/plain; charset=utf-8"))
			})

			It("should set the X-Content-Type-Options header", func() {
				Expect(recorder.Result().Header.Get("X-Content-Type-Options")).To(Equal("nosniff"))
			})
		})

		Context("when the response is not a success", func() {
			BeforeEach(func() {
				var err error
				_, err = tmpFile.Write([]byte(`{{template "notexists"}}`))
				Expect(err).NotTo(HaveOccurred())

				errorWriter, err = NewHTMLErrorWriterFromFile(tmpFile.Name())
				Expect(err).NotTo(HaveOccurred())

				errorWriter.WriteError(recorder, http.StatusBadRequest, "bad", logger)
			})

			It("should write the status code", func() {
				Expect(recorder.Result().StatusCode).To(Equal(http.StatusBadRequest))
			})

			It("should write the message as text", func() {
				Eventually(BufferReader(recorder.Result().Body)).Should(Say("400 Bad Request: bad"))
			})

			It("should delete the Connection header", func() {
				Expect(recorder.Result().Header.Get("Connection")).To(Equal(""))
			})

			It("should set the Content-Type header", func() {
				Expect(recorder.Result().Header.Get("Content-Type")).To(Equal("text/plain; charset=utf-8"))
			})

			It("should set the X-Content-Type-Options header", func() {
				Expect(recorder.Result().Header.Get("X-Content-Type-Options")).To(Equal("nosniff"))
			})
		})
	})

	Context("when the template renders", func() {
		var (
			err error
		)

		Context("when the response is a success", func() {
			BeforeEach(func() {
				_, err := tmpFile.Write([]byte(
					`{{ .Status }} {{ .StatusText }}: {{ .Message }}`,
				))
				Expect(err).NotTo(HaveOccurred())

				errorWriter, err = NewHTMLErrorWriterFromFile(tmpFile.Name())
				Expect(err).NotTo(HaveOccurred())

				errorWriter.WriteError(recorder, http.StatusOK, "hi", logger)
			})

			It("should not return an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("should write the status code", func() {
				Expect(recorder.Result().StatusCode).To(Equal(http.StatusOK))
			})

			It("should write the message as text", func() {
				Eventually(BufferReader(recorder.Result().Body)).Should(Say("200 OK: hi"))
			})

			It("should keep the Connection header", func() {
				Expect(recorder.Result().Header.Get("Connection")).To(Equal("dummy"))
			})

			It("should set the Content-Type header", func() {
				Expect(recorder.Result().Header.Get("Content-Type")).To(Equal("text/html; charset=utf-8"))
			})

			It("should set the X-Content-Type-Options header", func() {
				Expect(recorder.Result().Header.Get("X-Content-Type-Options")).To(Equal("nosniff"))
			})
		})

		Context("when the response is not a success", func() {
			BeforeEach(func() {
				_, err := tmpFile.Write([]byte(
					`{{ .Status }} {{ .StatusText }}: {{ .Message }}`,
				))
				Expect(err).NotTo(HaveOccurred())

				errorWriter, err = NewHTMLErrorWriterFromFile(tmpFile.Name())
				Expect(err).NotTo(HaveOccurred())

				errorWriter.WriteError(recorder, http.StatusBadRequest, "bad", logger)
			})

			It("should not return an error", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("should write the status code", func() {
				Expect(recorder.Result().StatusCode).To(Equal(http.StatusBadRequest))
			})

			It("should write the message as text", func() {
				Eventually(BufferReader(recorder.Result().Body)).Should(Say("400 Bad Request: bad"))
			})

			It("should delete the Connection header", func() {
				Expect(recorder.Result().Header.Get("Connection")).To(Equal(""))
			})

			It("should set the Content-Type header", func() {
				Expect(recorder.Result().Header.Get("Content-Type")).To(Equal("text/html; charset=utf-8"))
			})

			It("should set the X-Content-Type-Options header", func() {
				Expect(recorder.Result().Header.Get("X-Content-Type-Options")).To(Equal("nosniff"))
			})
		})
	})
})
