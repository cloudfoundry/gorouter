package secure_test

import (
	"encoding/base64"

	"github.com/cloudfoundry/gorouter/common/secure"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crypto", func() {

	var (
		aesGcm secure.Crypto
		key    []byte
		iv     []byte
	)

	BeforeEach(func() {
		var err error
		key, err = base64.StdEncoding.DecodeString("6TuytRTJPal4fXkAD5lwZA==")
		Expect(err).ToNot(HaveOccurred())
		aesGcm, err = secure.NewAesGCM(key)
		Expect(err).ToNot(HaveOccurred())
		iv = []byte("some iv")
	})

	Describe("Encrypt", func() {
		var (
			plainText = []byte("this is a secret message!")
			iv        = []byte("some iv")
		)

		Context("when the key and iv are valid", func() {
			It("encrypts the plain text into a cypher text and returns a nonce", func() {
				cipherText, nonce, err := aesGcm.Encrypt(plainText, iv)
				Expect(err).ToNot(HaveOccurred())
				Expect(cipherText).ToNot(Equal(plainText))
				Expect(nonce).ToNot(BeNil())
			})

			It("returns a different nonce for the same plain text", func() {
				cipherText, nonce, err := aesGcm.Encrypt(plainText, iv)
				Expect(err).ToNot(HaveOccurred())
				Expect(cipherText).ToNot(Equal(plainText))
				Expect(nonce).ToNot(BeNil())

				cipherText2, nonce2, err := aesGcm.Encrypt(plainText, iv)
				Expect(err).ToNot(HaveOccurred())
				Expect(cipherText).ToNot(Equal(cipherText2))
				Expect(nonce).ToNot(Equal(nonce2))
			})
		})

		Context("when the key is invalid", func() {
			BeforeEach(func() {
				key = []byte("invalid key")
			})

			It("returns an invalid key size error", func() {
				_, err := secure.NewAesGCM(key)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("invalid key size"))
			})
		})
	})

	Describe("Decrypt", func() {
		var (
			plainText  = []byte("this is a secret message!")
			iv         = []byte("some iv")
			cipherText []byte
			nonce      []byte
		)

		BeforeEach(func() {
			var err error
			cipherText, nonce, err = aesGcm.Encrypt(plainText, iv)
			Expect(err).ToNot(HaveOccurred())
			Expect(cipherText).ToNot(Equal(plainText))
			Expect(nonce).ToNot(BeNil())
		})

		Context("when using correct key, iv, and nonce", func() {
			It("decrypts the cipher text", func() {
				decryptedText, err := aesGcm.Decrypt(cipherText, iv, nonce)
				Expect(err).ToNot(HaveOccurred())
				Expect(decryptedText).To(Equal(plainText))
			})
		})

		Context("when using an invalid key", func() {
			It("returns an error", func() {
				otherKey := []byte("0123456789ABCDEF")

				otherAesGcm, err := secure.NewAesGCM(otherKey)
				Expect(err).ToNot(HaveOccurred())

				decryptedText, err := otherAesGcm.Decrypt(cipherText, iv, nonce)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("authentication failed"))
				Expect(decryptedText).ToNot(Equal(plainText))
			})
		})

		Context("when using an invalid nonce", func() {
			It("returns an error", func() {
				otherNonce := []byte("0123456789AB")
				decryptedText, err := aesGcm.Decrypt(cipherText, iv, otherNonce)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("authentication failed"))
				Expect(decryptedText).ToNot(Equal(plainText))
			})
		})

		Context("when using an invalid iv", func() {
			It("returns an error", func() {
				otherIv := []byte("0123456789ABCDEF")
				decryptedText, err := aesGcm.Decrypt(cipherText, otherIv, nonce)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("authentication failed"))
				Expect(decryptedText).ToNot(Equal(plainText))
			})
		})
	})
})
