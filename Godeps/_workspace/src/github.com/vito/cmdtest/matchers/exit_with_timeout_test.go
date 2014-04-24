package cmdtest_matchers_test

import (
	"time"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/vito/cmdtest/matchers"
)

var _ = Describe("ExitWithTimeout Matcher", func() {
	It("matches if the program exits with the expected status within the specified timeout", func() {
		Expect(Run("ls", "/that/is/one/unlikely/path")).To(ExitWithTimeout(1, 2*time.Second))
	})

	It("does not match if the program does not exit within the timeout", func() {
		Expect(Run("bash", "-c", "sleep 2 && echo SUCCESS")).NotTo(ExitWithTimeout(0, 1*time.Second))
	})

	It("does not match if the program exits but without the expected status", func() {
		Expect(Run("echo", "SUCCESS")).NotTo(ExitWithTimeout(1, 1*time.Second))
	})
})
