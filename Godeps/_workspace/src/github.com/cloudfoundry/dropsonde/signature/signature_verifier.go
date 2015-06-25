// Package signature signs and validates dropsonde messages.

// Messages are prepended with a HMAC SHA256 signature (the signature makes up
// the first 32 bytes of a signed message; the remainder is the original message
// in cleartext).
package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/loggregatorlib/cfcomponent/instrumentation"
	"sync/atomic"
)

const SIGNATURE_LENGTH = 32

// A SignatureVerifier is a self-instrumenting pipeline object that validates
// and removes signatures.
type SignatureVerifier interface {
	instrumentation.Instrumentable
	Run(inputChan <-chan []byte, outputChan chan<- []byte)
}

// NewSignatureVerifier returns a SignatureVerifier with the provided logger and
// shared signing secret.
func NewSignatureVerifier(logger *gosteno.Logger, sharedSecret string) SignatureVerifier {
	return &signatureVerifier{
		logger:       logger,
		sharedSecret: sharedSecret,
	}
}

type signatureVerifier struct {
	logger                     *gosteno.Logger
	sharedSecret               string
	missingSignatureErrorCount uint64
	invalidSignatureErrorCount uint64
	validSignatureCount        uint64
}

// Run validates signatures. It consumes signed messages from inputChan,
// verifies the signature, and sends the message (sans signature) to outputChan.
// Invalid messages are dropped and nothing is sent to outputChan. Thus a reader
// of outputChan is guaranteed to receive only messages with a valid signature.
//
// Run blocks on sending to outputChan, so the channel must be drained for the
// function to continue consuming from inputChan.
func (v *signatureVerifier) Run(inputChan <-chan []byte, outputChan chan<- []byte) {
	for signedMessage := range inputChan {
		if len(signedMessage) < SIGNATURE_LENGTH {
			v.logger.Warnf("signatureVerifier: missing signature for message %v", signedMessage)
			incrementCount(&v.missingSignatureErrorCount)
			continue
		}

		signature, message := signedMessage[:SIGNATURE_LENGTH], signedMessage[SIGNATURE_LENGTH:]
		if v.verifyMessage(message, signature) {
			outputChan <- message
			incrementCount(&v.validSignatureCount)
			v.logger.Debugf("signatureVerifier: valid signature %v for message %v", signature, message)
		} else {
			v.logger.Warnf("signatureVerifier: invalid signature %v for message %v", signature, message)
			incrementCount(&v.invalidSignatureErrorCount)
		}
	}
}

func (v *signatureVerifier) verifyMessage(message, signature []byte) bool {
	expectedMAC := generateSignature(message, []byte(v.sharedSecret))
	return hmac.Equal(signature, expectedMAC)
}

func (v *signatureVerifier) metrics() []instrumentation.Metric {
	return []instrumentation.Metric{
		instrumentation.Metric{Name: "missingSignatureErrors", Value: atomic.LoadUint64(&v.missingSignatureErrorCount)},
		instrumentation.Metric{Name: "invalidSignatureErrors", Value: atomic.LoadUint64(&v.invalidSignatureErrorCount)},
		instrumentation.Metric{Name: "validSignatures", Value: atomic.LoadUint64(&v.validSignatureCount)},
	}
}

func (v *signatureVerifier) Emit() instrumentation.Context {
	return instrumentation.Context{
		Name:    "signatureVerifier",
		Metrics: v.metrics(),
	}
}

// SignMessage returns a message signed with the provided secret, with the
// signature prepended to the original message.
func SignMessage(message, secret []byte) []byte {
	signature := generateSignature(message, secret)
	return append(signature, message...)
}

func generateSignature(message, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(message)
	return mac.Sum(nil)
}

func incrementCount(count *uint64) {
	atomic.AddUint64(count, 1)
}
