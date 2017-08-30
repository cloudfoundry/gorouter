package round_tripper_test

import (
	"errors"
	"net"

	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("roundTripperRetryableClassifier", func() {
	Context("IsRetryable", func() {
		var retry round_tripper.RoundTripperRetryableClassifier
		var err error
		BeforeEach(func() {
			retry = round_tripper.RoundTripperRetryableClassifier{}
		})
		Context("When error is a dial error", func() {
			BeforeEach(func() {
				err = &net.OpError{
					Err: errors.New("error"),
					Op:  "dial",
				}
			})
			It("returns true", func() {
				Expect(retry.IsRetryable(err)).To(BeTrue())
			})
		})
		Context("When error is a 'read: connection reset by peer' error", func() {
			BeforeEach(func() {
				err = &net.OpError{
					Err: errors.New("read: connection reset by peer"),
					Op:  "read",
				}
			})
			It("returns true", func() {
				Expect(retry.IsRetryable(err)).To(BeTrue())
			})
		})
		Context("When error is anything other than a dial or 'read: connection reset by peer'", func() {
			BeforeEach(func() {
				err = &net.OpError{
					Err: errors.New("other error"),
					Op:  "write",
				}
			})
			It("returns true", func() {
				Expect(retry.IsRetryable(err)).To(BeFalse())
			})
		})
	})
})
