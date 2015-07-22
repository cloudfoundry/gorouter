package route_service

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/cloudfoundry/gorouter/common/secure"
)

type Signature struct {
	RequestedTime time.Time `json:"requested_time"`
}

type Metadata struct {
	IV    []byte `json:"iv"`
	Nonce []byte `json:"nonce"`
}

func BuildSignatureAndMetadata(crypto secure.Crypto) (string, string, error) {
	signature := Signature{RequestedTime: time.Now()}
	signatureJson, err := json.Marshal(&signature)
	if err != nil {
		return "", "", err
	}

	signatureJsonEncrypted, nonce, iv, err := crypto.Encrypt(signatureJson)

	metadata := Metadata{
		IV:    iv,
		Nonce: nonce,
	}

	metadataJson, err := json.Marshal(&metadata)
	if err != nil {
		return "", "", err
	}

	metadataHeader := base64.URLEncoding.EncodeToString(metadataJson)
	signatureHeader := base64.URLEncoding.EncodeToString(signatureJsonEncrypted)

	return signatureHeader, metadataHeader, nil
}

func SignatureFromHeaders(signatureHeader, metadataHeader string, crypto secure.Crypto) (Signature, error) {
	metadata := Metadata{}
	signature := Signature{}

	metadataDecoded, err := base64.URLEncoding.DecodeString(metadataHeader)
	if err != nil {
		return signature, err
	}

	err = json.Unmarshal([]byte(metadataDecoded), &metadata)
	signatureDecoded, err := base64.URLEncoding.DecodeString(signatureHeader)
	if err != nil {
		return signature, err
	}

	signatureDecrypted, err := crypto.Decrypt(signatureDecoded, metadata.Nonce, metadata.IV)
	if err != nil {
		return signature, err
	}

	err = json.Unmarshal([]byte(signatureDecrypted), &signature)

	return signature, err
}
