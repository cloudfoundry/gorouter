package fails_test

import (
	"errors"

	"code.cloudfoundry.org/gorouter/proxy/fails"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RetryableClassifier", func() {
	It("matches any of the classifiers in the RetryOnAny set", func() {
		rc := &fails.Retriable{
			RetryOnAny: []fails.Classifier{
				fails.ClassifierFunc(func(err error) bool {
					return err.Error() == "i'm a teapot"
				}),
				fails.ClassifierFunc(func(err error) bool {
					return err.Error() == "i'm a tomato"
				}),
			},
		}

		Expect(rc.Classify(errors.New("i'm a teapot"))).To(BeTrue())
		Expect(rc.Classify(errors.New("i'm a tomato"))).To(BeTrue())
		Expect(rc.Classify(errors.New("i'm a potato"))).To(BeFalse())
	})
})
