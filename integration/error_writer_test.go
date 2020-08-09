package integration

import (
	"fmt"
	"io/ioutil"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
)

var _ = Describe("Error Writers", func() {
	const (
		hostname = "error-writers.cloudfoundry.org"
	)

	var (
		testState *testState

		statusCode int
		body       []byte

		doRequest = func() {
			req := testState.newRequest(fmt.Sprintf("http://not-%s", hostname))

			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())

			statusCode = resp.StatusCode

			body, err = ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			resp.Body.Close()
		}
	)

	BeforeEach(func() {
		testState = NewTestState()
	})

	AfterEach(func() {
		testState.StopAndCleanup()
	})

	Context("when using plaintext error writer", func() {
		BeforeEach(func() {
		})

		JustBeforeEach(func() {
			testState.StartGorouterOrFail()
		})

		BeforeEach(func() {
			testState.cfg.HTMLErrorTemplateFile = ""
		})

		It("responds with a plaintext error message", func() {
			doRequest()

			Expect(statusCode).To(Equal(404))

			Expect(string(body)).To(Equal(fmt.Sprintf(
				"404 Not Found: Requested route ('not-%s') does not exist.\n",
				hostname,
			)))
		})
	})

	Context("when using HTML error writer", func() {
		Context("when the template does not exist", func() {
			BeforeEach(func() {
				testState.cfg.HTMLErrorTemplateFile = "/path/to/non/file"
			})

			It("should log a fatal error", func() {
				session := testState.StartGorouter()

				Eventually(session).Should(Say("Could not read HTML error template file"))
				Eventually(session).Should(Say("/path/to/non/file"))

				Eventually(session).Should(Exit())
				Expect(session.ExitCode()).To(Equal(1))
			})
		})

		Context("when the template exists", func() {
			var (
				tmpFile *os.File
			)

			BeforeEach(func() {
				tpl := `<html><body>{{ .Message }}</body></html>`

				var err error
				tmpFile, err = ioutil.TempFile(os.TempDir(), "html-err-tpl")
				Expect(err).NotTo(HaveOccurred())

				testState.cfg.HTMLErrorTemplateFile = tmpFile.Name()

				_, err = tmpFile.Write([]byte(tpl))
				Expect(err).NotTo(HaveOccurred())
			})

			JustBeforeEach(func() {
				testState.StartGorouterOrFail()
			})

			AfterEach(func() {
				os.Remove(tmpFile.Name())
			})

			It("responds with a templated error message", func() {
				doRequest()

				Expect(statusCode).To(Equal(404))

				Expect(string(body)).To(Equal(fmt.Sprintf(
					"<html><body>Requested route (&#39;not-%s&#39;) does not exist.</body></html>",
					hostname,
				)))
			})
		})

		Context("when the template references an HTTP header", func() {
			var (
				tmpFile *os.File
			)

			BeforeEach(func() {
				tpl := `<html><body>Code: {{ .Status }} ; Cause: {{ .Header.Get "X-Cf-RouterError" }}</body></html>`

				var err error
				tmpFile, err = ioutil.TempFile(os.TempDir(), "html-err-tpl")
				Expect(err).NotTo(HaveOccurred())

				testState.cfg.HTMLErrorTemplateFile = tmpFile.Name()

				_, err = tmpFile.Write([]byte(tpl))
				Expect(err).NotTo(HaveOccurred())
			})

			JustBeforeEach(func() {
				testState.StartGorouterOrFail()
			})

			AfterEach(func() {
				os.Remove(tmpFile.Name())
			})

			It("responds with a templated error message", func() {
				doRequest()

				Expect(statusCode).To(Equal(404))

				Expect(string(body)).To(ContainSubstring("Code: 404"))
				Expect(string(body)).To(ContainSubstring("Cause: unknown_route"))
			})
		})
	})
})
