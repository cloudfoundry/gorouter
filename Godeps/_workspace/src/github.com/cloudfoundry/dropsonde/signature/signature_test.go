package signature_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"github.com/cloudfoundry/dropsonde/signature"
	"github.com/cloudfoundry/loggregatorlib/cfcomponent/instrumentation/testhelpers"
	"github.com/cloudfoundry/loggregatorlib/loggertesthelper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("SignatureVerifier", func() {
	var (
		inputChan         chan []byte
		outputChan        chan []byte
		runComplete       chan struct{}
		signatureVerifier signature.SignatureVerifier
	)

	BeforeEach(func() {
		inputChan = make(chan []byte, 10)
		outputChan = make(chan []byte, 10)
		runComplete = make(chan struct{})
		signatureVerifier = signature.NewSignatureVerifier(loggertesthelper.Logger(), "valid-secret")

		go func() {
			signatureVerifier.Run(inputChan, outputChan)
			close(runComplete)
		}()
	})

	AfterEach(func() {
		close(inputChan)
		Eventually(runComplete).Should(BeClosed())
	})

	It("discards messages less than 32 bytes long", func() {
		message := make([]byte, 1)
		inputChan <- message
		Consistently(outputChan).ShouldNot(Receive())
	})

	It("discards messages when verification fails", func() {
		message := make([]byte, 33)
		inputChan <- message
		Consistently(outputChan).ShouldNot(Receive())
	})

	It("passes through messages with valid signature", func() {
		message := []byte{1, 2, 3}
		mac := hmac.New(sha256.New, []byte("valid-secret"))
		mac.Write(message)
		signature := mac.Sum(nil)

		signedMessage := append(signature, message...)

		inputChan <- signedMessage
		outputMessage := <-outputChan
		Expect(outputMessage).To(Equal(message))
	})

	Context("metrics", func() {
		It("emits the correct metrics context", func() {
			Expect(signatureVerifier.Emit().Name).To(Equal("signatureVerifier"))
		})

		It("emits a missing signature error counter", func() {
			inputChan <- []byte{1, 2, 3}
			testhelpers.EventuallyExpectMetric(signatureVerifier, "missingSignatureErrors", 1)
		})

		It("emits an invalid signature error counter", func() {
			inputChan <- make([]byte, 32)
			testhelpers.EventuallyExpectMetric(signatureVerifier, "invalidSignatureErrors", 1)
		})

		It("emits an valid signature counter", func() {
			message := []byte{1, 2, 3}
			mac := hmac.New(sha256.New, []byte("valid-secret"))
			mac.Write(message)
			signature := mac.Sum(nil)

			signedMessage := append(signature, message...)
			inputChan <- signedMessage
			testhelpers.EventuallyExpectMetric(signatureVerifier, "validSignatures", 1)
		})
	})
})
