package route_service_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/cloudfoundry/gorouter/common/secure/fakes"
	"github.com/cloudfoundry/gorouter/route_service"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Route Service Header", func() {
	var (
		crypto = new(fakes.FakeCrypto)
	)

	BeforeEach(func() {
		crypto.DecryptStub = func(cipherText, iv, nonce []byte) ([]byte, error) {
			decryptedStr := string(cipherText)

			decryptedStr = strings.Replace(decryptedStr, "encrypted", "", -1)
			decryptedStr = strings.Replace(decryptedStr, string(iv), "", -1)
			decryptedStr = strings.Replace(decryptedStr, string(nonce), "", -1)
			return []byte(decryptedStr), nil
		}

		crypto.EncryptStub = func(plainText []byte) ([]byte, []byte, []byte, error) {
			nonce := []byte("some-nonce")
			iv := []byte("some-iv")
			cipherText := append(plainText, "encrypted"...)
			cipherText = append(cipherText, nonce...)
			cipherText = append(cipherText, iv...)
			return cipherText, nonce, iv, nil
		}
	})

	Describe("Build Signature and Metadata", func() {
		It("builds signature and metadata headers", func() {
			signature, metadata, err := route_service.BuildSignatureAndMetadata(crypto)
			Expect(err).ToNot(HaveOccurred())
			Expect(signature).ToNot(BeNil())
			metadataDecoded, err := base64.URLEncoding.DecodeString(metadata)
			Expect(err).ToNot(HaveOccurred())
			metadataStruct := route_service.Metadata{}
			err = json.Unmarshal([]byte(metadataDecoded), &metadataStruct)
			Expect(err).ToNot(HaveOccurred())
			Expect(metadataStruct.Nonce).To(Equal([]byte("some-nonce")))
			Expect(metadataStruct.IV).To(Equal([]byte("some-iv")))
		})
	})

	Describe("Parse signature from headers", func() {

		It("parses signature from signature and metadata headers", func() {
			signatureHeader := "eyJyZXF1ZXN0ZWRfdGltZSI6IjIwMTUtMDctMjNUMTA6NDg6MDguMjQwMDMwNzIyLTA3OjAwIn1lbmNyeXB0ZWRzb21lLW5vbmNlc29tZS1pdg=="
			metadataHeader := "eyJpdiI6ImMyOXRaUzFwZGc9PSIsIm5vbmNlIjoiYzI5dFpTMXViMjVqWlE9PSJ9"
			signature, err := route_service.SignatureFromHeaders(signatureHeader, metadataHeader, crypto)
			Expect(err).ToNot(HaveOccurred())
			Expect(signature.RequestedTime.Sub(time.Unix(1437673688, 240030722))).To(Equal(time.Duration(0)))
		})
	})

})
