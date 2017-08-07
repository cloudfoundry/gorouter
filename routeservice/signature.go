package routeservice

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
)

type Signature struct {
	ForwardedUrl  string    `json:"forwarded_url"`
	RequestedTime time.Time `json:"requested_time"`
}

type Metadata struct {
	Nonce []byte `json:"nonce"`
}

func BuildSignatureAndMetadata(crypto secure.Crypto, signature *Signature) (string, string, error) {
	signatureJson, err := json.Marshal(&signature)
	if err != nil {
		return "", "", err
	}

	signatureJsonEncrypted, nonce, err := crypto.Encrypt(signatureJson)
	if err != nil {
		return "", "", err
	}

	metadata := Metadata{
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

	if metadataHeader == "" {
		return signature, errors.New("No metadata found")
	}

	metadataDecoded, err := base64.URLEncoding.DecodeString(metadataHeader)
	if err != nil {
		return signature, err
	}

	err = json.Unmarshal(metadataDecoded, &metadata)
	signatureDecoded, err := base64.URLEncoding.DecodeString(signatureHeader)
	if err != nil {
		return signature, err
	}

	signatureDecrypted, err := crypto.Decrypt(signatureDecoded, metadata.Nonce)
	if err != nil {
		return signature, err
	}

	err = json.Unmarshal([]byte(signatureDecrypted), &signature)

	return signature, err
}
