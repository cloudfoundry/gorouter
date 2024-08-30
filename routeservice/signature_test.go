package routeservice_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/common/secure/fakes"
	"code.cloudfoundry.org/gorouter/routeservice"
)

var _ = Describe("Route Service Signature", func() {
	var (
		crypto            = new(fakes.FakeCrypto)
		signatureContents *routeservice.SignatureContents
	)

	BeforeEach(func() {
		crypto.DecryptStub = func(cipherText, nonce []byte) ([]byte, error) {
			decryptedStr := string(cipherText)

			decryptedStr = strings.Replace(decryptedStr, "encrypted", "", -1)
			decryptedStr = strings.Replace(decryptedStr, string(nonce), "", -1)
			return []byte(decryptedStr), nil
		}

		crypto.EncryptStub = func(plainText []byte) ([]byte, []byte, error) {
			nonce := []byte("some-nonce")
			cipherText := append(plainText, "encrypted"...)
			cipherText = append(cipherText, nonce...)
			return cipherText, nonce, nil
		}

		signatureContents = &routeservice.SignatureContents{RequestedTime: time.Now()}
	})

	Describe("Build Signature and Metadata", func() {
		It("builds signature and metadata headers", func() {
			signatureHeader, metadata, err := routeservice.BuildSignatureAndMetadata(crypto, signatureContents)
			Expect(err).ToNot(HaveOccurred())
			Expect(signatureHeader).ToNot(BeEmpty())
			metadataDecoded, err := base64.URLEncoding.DecodeString(metadata)
			Expect(err).ToNot(HaveOccurred())
			metadataStruct := routeservice.Metadata{}
			err = json.Unmarshal([]byte(metadataDecoded), &metadataStruct)
			Expect(err).ToNot(HaveOccurred())
			Expect(metadataStruct.Nonce).To(Equal([]byte("some-nonce")))
		})

		Context("when unable to encrypt the signature", func() {
			BeforeEach(func() {
				crypto.EncryptReturns([]byte{}, []byte{}, errors.New("No entropy"))
			})

			It("returns an error", func() {
				_, _, err := routeservice.BuildSignatureAndMetadata(crypto, signatureContents)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("Parse headers into signatureContent", func() {
		var (
			signatureHeader string
			metadataHeader  string
		)

		BeforeEach(func() {
			var err error
			signatureHeader, metadataHeader, err = routeservice.BuildSignatureAndMetadata(crypto, signatureContents)
			Expect(err).ToNot(HaveOccurred())
		})

		It("parses signatureContents from signature and metadata headers", func() {
			decryptedSignature, err := routeservice.SignatureContentsFromHeaders(signatureHeader, metadataHeader, crypto)
			Expect(err).ToNot(HaveOccurred())
			Expect(signatureContents.RequestedTime.Sub(decryptedSignature.RequestedTime)).To(Equal(time.Duration(0)))
		})
	})

})
