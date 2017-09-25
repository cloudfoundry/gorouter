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
		rc := &round_tripper.Retriable{
			RetryOnAny: []error_classifiers.Classifier{
				error_classifiers.ClassifierFunc(func(err error) bool {
					return err.Error() == "i'm a teapot"
				}),
				error_classifiers.ClassifierFunc(func(err error) bool {
					return err.Error() == "i'm a tomato"
				}),
			},
		}

		Expect(rc.Classify(errors.New("i'm a teapot"))).To(BeTrue())
		Expect(rc.Classify(errors.New("i'm a tomato"))).To(BeTrue())
		Expect(rc.Classify(errors.New("i'm a potato"))).To(BeFalse())
	})
})
