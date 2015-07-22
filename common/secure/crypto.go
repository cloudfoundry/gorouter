package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
)

type Crypto interface {
	Encrypt(plainText []byte) (cipherText []byte, nonce []byte, iv []byte, err error)
	Decrypt(cipherText, nonce, iv []byte) ([]byte, error)
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

func (gcm *AesGCM) Encrypt(plainText []byte) (cipherText, nonce []byte, iv []byte, err error) {
	nonce, err = gcm.generateNonce()
	if err != nil {
		return nil, nil, nil, err
	}

	iv, err = randomBytes(12)
	if err != nil {
		return nil, nil, nil, err
	}

	cipherText = gcm.Seal(nil, nonce, plainText, iv)

	return cipherText, nonce, iv, nil
}

func (gcm *AesGCM) Decrypt(cipherText, nonce, iv []byte) ([]byte, error) {
	plainText, err := gcm.Open(nil, nonce, cipherText, iv)
	if err != nil {
		return nil, err
	}

	return plainText, nil
}

func (gcm *AesGCM) generateNonce() ([]byte, error) {
	return randomBytes(uint(gcm.NonceSize()))
}

func randomBytes(size uint) ([]byte, error) {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	return b, nil
}
