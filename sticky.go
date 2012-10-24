package router

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
)

const (
	SessionLength                 = 32
	MaxBase64EncodedSessionLength = SessionLength << 1
)

type SessionEncoder struct {
	cipher   cipher.Block
	encoding *base64.Encoding
}

func NewSessionEncoder(cc cipher.Block, encoding *base64.Encoding) *SessionEncoder {
	se := new(SessionEncoder)
	se.cipher = cc
	se.encoding = encoding

	return se
}

func NewAESSessionEncoder(sessionKey []byte, encoding *base64.Encoding) (*SessionEncoder, error) {
	cc, err := aes.NewCipher(sessionKey)
	if err != nil {
		return nil, err
	}

	return NewSessionEncoder(cc, encoding), nil
}

func (se *SessionEncoder) getStickyCookie(rm *registerMessage) string {
	if rm.Sticky != "" {
		log.Debug("found sticky in cache")
		return rm.Sticky
	} else {
		log.Debug("save sticky session in droplet cache")
		rm.Sticky = se.encryptStickyCookie(rm.Host, rm.Port)
	}

	return rm.Sticky
}

func (se *SessionEncoder) encryptStickyCookie(host string, port uint16) string {
	hostPort := fmt.Sprintf("%s:%d", host, port)
	log.Debugf("encrypting %s\n", hostPort)

	var hp [SessionLength]byte
	hp[0] = byte(len(hostPort))
	copy(hp[1:], hostPort)

	var c [SessionLength]byte
	se.encrypt(c[:], hp[:])

	msg := se.encoding.EncodeToString(c[:])

	return msg
}

func (se *SessionEncoder) decryptStickyCookie(sticky string) (string, uint16) {
	log.Debugf("decrypting %s\n", sticky)

	if len(sticky) > MaxBase64EncodedSessionLength {
		log.Debugf("sticky session length(%d) exceeds SessionLength(%d)",
			len(sticky),
			MaxBase64EncodedSessionLength)
		return "", 0
	}

	c, err := se.encoding.DecodeString(sticky)
	if err != nil || len(c) != SessionLength {
		log.Debug("invalid token")
		return "", 0
	}

	var bytes [SessionLength]byte
	se.decrypt(bytes[:], c)

	length := int(bytes[0])
	if length > SessionLength-1 {
		log.Debug("invalid token")
		return "", 0
	}
	hostPort := string(bytes[1 : length+1])

	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		log.Debugf("error parsing host port: %s\n", err)
		return "", 0
	}

	p, err := strconv.Atoi(port)
	if err != nil {
		log.Debugf("error parsing host port: %s\n", err)
		return "", 0
	}

	return host, uint16(p)
}

func (se *SessionEncoder) encrypt(dst, src []byte) {
	blockSize := se.cipher.BlockSize()
	for i := 0; i < len(src); i += blockSize {
		j := i + blockSize
		if j > len(src) {
			j = len(src)
		}

		se.cipher.Encrypt(dst[i:j], src[i:j])
	}
}

func (se *SessionEncoder) decrypt(dst, src []byte) {
	blockSize := se.cipher.BlockSize()
	for i := 0; i < len(src); i += blockSize {
		j := i + blockSize
		if j > len(src) {
			j = len(src)
		}

		se.cipher.Decrypt(dst[i:j], src[i:j])
	}
}
