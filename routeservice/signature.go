package routeservice

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
)

type SignatureContents struct {
	ForwardedUrl  string    `json:"forwarded_url"`
	RequestedTime time.Time `json:"requested_time"`
}

type Metadata struct {
	Nonce []byte `json:"nonce"`
}

func BuildSignatureAndMetadata(
	crypto secure.Crypto,
	signatureContents *SignatureContents,
) (string, string, error) {

	signatureContentsJson, err := json.Marshal(&signatureContents)
	if err != nil {
		return "", "", err
	}

	signatureJsonEncrypted, nonce, err := crypto.Encrypt(signatureContentsJson)
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

func SignatureContentsFromHeaders(signatureHeader, metadataHeader string, crypto secure.Crypto) (SignatureContents, error) {
	metadata := Metadata{}
	signatureContents := SignatureContents{}

	if metadataHeader == "" {
		return signatureContents, errors.New("No metadata found")
	}

	metadataDecoded, err := base64.URLEncoding.DecodeString(metadataHeader)
	if err != nil {
		return signatureContents, err
	}

	err = json.Unmarshal(metadataDecoded, &metadata)
	if err != nil {
		return signatureContents, err
	}

	signatureDecoded, err := base64.URLEncoding.DecodeString(signatureHeader)
	if err != nil {
		return signatureContents, err
	}

	signatureDecrypted, err := crypto.Decrypt(signatureDecoded, metadata.Nonce)
	if err != nil {
		return signatureContents, err
	}

	err = json.Unmarshal([]byte(signatureDecrypted), &signatureContents)

	return signatureContents, err
}
