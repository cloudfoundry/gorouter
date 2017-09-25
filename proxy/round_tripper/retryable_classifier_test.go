package round_tripper_test

import (
	"errors"

	"code.cloudfoundry.org/gorouter/proxy/round_tripper"

	"code.cloudfoundry.org/gorouter/proxy/error_classifiers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RetryableClassifier", func() {
	It("matches against any of the classifiers in the RetryOnAny set", func() {
		rc := &round_tripper.RoundTripperRetryableClassifier{
			RetryOnAny: []error_classifiers.Classifier{
				func(err error) bool {
					return err.Error() == "i'm a teapot"
				},
				func(err error) bool {
					return err.Error() == "i'm a tomato"
				},
			},
		}

		Expect(rc.IsRetryable(errors.New("i'm a teapot"))).To(BeTrue())
		Expect(rc.IsRetryable(errors.New("i'm a tomato"))).To(BeTrue())
		Expect(rc.IsRetryable(errors.New("i'm a potato"))).To(BeFalse())
	})
})
