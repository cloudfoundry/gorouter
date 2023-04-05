package fails_test

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"

	"code.cloudfoundry.org/gorouter/proxy/fails"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/proxy/fails"
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

	Describe("retriable", func() {
		It("matches retriable errors", func() {
			rc := fails.RetriableClassifiers

			Expect(rc.Classify(&net.OpError{Op: "dial"})).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, &net.OpError{Op: "dial"}))).To(BeTrue())
			Expect(rc.Classify(&net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")})).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, &net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")}))).To(BeTrue())
			Expect(rc.Classify(&net.OpError{Op: "remote error", Err: errors.New("tls: handshake failure")})).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, &net.OpError{Op: "remote error", Err: errors.New("tls: handshake failure")}))).To(BeTrue())
			Expect(rc.Classify(errors.New("net/http: TLS handshake timeout"))).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, errors.New("net/http: TLS handshake timeout")))).To(BeTrue())
			Expect(rc.Classify(tls.RecordHeaderError{})).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, tls.RecordHeaderError{}))).To(BeTrue())
			Expect(rc.Classify(x509.HostnameError{})).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, x509.HostnameError{}))).To(BeTrue())
			Expect(rc.Classify(x509.UnknownAuthorityError{})).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, x509.UnknownAuthorityError{}))).To(BeTrue())
			Expect(rc.Classify(x509.CertificateInvalidError{Reason: x509.Expired})).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, x509.CertificateInvalidError{Reason: x509.Expired}))).To(BeTrue())
			Expect(rc.Classify(errors.New("i'm a potato"))).To(BeFalse())
			Expect(rc.Classify(fails.IdempotentRequestEOFError)).To(BeTrue())
			Expect(rc.Classify(fails.IncompleteRequestError)).To(BeTrue())
			Expect(rc.Classify(fmt.Errorf("%w (%w)", fails.IncompleteRequestError, x509.HostnameError{}))).To(BeTrue())
		})
	})

	Describe("prunable", func() {
		It("matches hostname mismatch", func() {
			pc := fails.PrunableClassifiers

			Expect(pc.Classify(&net.OpError{Op: "dial"})).To(BeTrue())
			Expect(pc.Classify(&net.OpError{Op: "read", Err: errors.New("read: connection reset by peer")})).To(BeFalse())
			Expect(pc.Classify(&net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")})).To(BeTrue())
			Expect(pc.Classify(&net.OpError{Op: "remote error", Err: errors.New("tls: handshake failure")})).To(BeTrue())
			Expect(pc.Classify(errors.New("net/http: TLS handshake timeout"))).To(BeTrue())
			Expect(pc.Classify(tls.RecordHeaderError{})).To(BeTrue())
			Expect(pc.Classify(x509.HostnameError{})).To(BeTrue())
			Expect(pc.Classify(x509.UnknownAuthorityError{})).To(BeTrue())
			Expect(pc.Classify(x509.CertificateInvalidError{Reason: x509.Expired})).To(BeTrue())
			Expect(pc.Classify(errors.New("i'm a potato"))).To(BeFalse())
			Expect(pc.Classify(fails.IdempotentRequestEOFError)).To(BeTrue())
			Expect(pc.Classify(fails.IncompleteRequestError)).To(BeTrue())
		})
	})
})
