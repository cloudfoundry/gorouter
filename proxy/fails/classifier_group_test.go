package fails_test

import (
	"errors"

	"crypto/x509"
	"net"

	"crypto/tls"

	"code.cloudfoundry.org/gorouter/proxy/fails"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ClassifierGroup", func() {
	It("matches any of the classifiers in the RetryOnAny set", func() {
		cg := &fails.ClassifierGroup{
			fails.ClassifierFunc(func(err error) bool {
				return err.Error() == "i'm a teapot"
			}),
			fails.ClassifierFunc(func(err error) bool {
				return err.Error() == "i'm a tomato"
			}),
		}

		Expect(cg.Classify(errors.New("i'm a teapot"))).To(BeTrue())
		Expect(cg.Classify(errors.New("i'm a tomato"))).To(BeTrue())
		Expect(cg.Classify(errors.New("i'm a potato"))).To(BeFalse())
	})

	Describe("ErrorTypes", func() {
		It("matches the errors", func() {
			rc := fails.ErrorTypes

			Expect(rc.Classify(&net.OpError{Op: "dial"})).To(BeTrue())
			Expect(rc.Classify(&net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")})).To(BeTrue())
			Expect(rc.Classify(&net.OpError{Op: "remote error", Err: errors.New("tls: handshake failure")})).To(BeTrue())
			Expect(rc.Classify(tls.RecordHeaderError{})).To(BeTrue())
			Expect(rc.Classify(x509.HostnameError{})).To(BeTrue())
			Expect(rc.Classify(x509.UnknownAuthorityError{})).To(BeTrue())
			Expect(rc.Classify(errors.New("i'm a potato"))).To(BeFalse())
		})
	})
})
