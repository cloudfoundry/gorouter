package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
)

type Crypto interface {
	Encrypt(plainText, iv []byte) (cipherText, nonce []byte, err error)
	Decrypt(cipherText, iv, nonce []byte) ([]byte, error)
}

type AesGCM struct {
	cipher.AEAD
}

func NewAesGCM(key []byte) (*AesGCM, error) {
	aes, err := aes.NewCipher(key)
	if err != nil {
		return &AesGCM{}, err
	}

	aead, err := cipher.NewGCM(aes)
	if err != nil {
		return &AesGCM{}, err
	}

	aesGCM := AesGCM{aead}
	return &aesGCM, nil
}

func (gcm *AesGCM) Encrypt(plainText, iv []byte) (cipherText, nonce []byte, err error) {
	nonce, err = gcm.generateNonce()
	if err != nil {
		return nil, nil, err
	}

	cipherText = gcm.Seal(nil, nonce, plainText, iv)

	return cipherText, nonce, nil
}

func (gcm *AesGCM) Decrypt(cipherText, iv, nonce []byte) ([]byte, error) {
	plainText, err := gcm.Open(nil, nonce, cipherText, iv)
	if err != nil {
		return nil, err
	}

	return plainText, nil
}

func (gcm *AesGCM) generateNonce() ([]byte, error) {
	b := make([]byte, gcm.NonceSize())
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	return b, nil
}
