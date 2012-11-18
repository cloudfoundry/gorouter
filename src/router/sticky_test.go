package router

import (
	"encoding/base64"
	. "launchpad.net/gocheck"
)

type StickySuite struct {
	se *SessionEncoder
}

var _ = Suite(&StickySuite{})

func (s *StickySuite) SetUpSuite(c *C) {
	se, err := NewAESSessionEncoder([]byte("14fbc303b76bacd1e0a3ab641c11d114"), base64.StdEncoding)
	c.Assert(err, IsNil)

	s.se = se
}

func (s *StickySuite) TestEncryption(c *C) {
	host := "123.123.123.123"
	port := uint16(12345)

	session := s.se.encryptStickyCookie(host, port)
	nHost, nPort := s.se.decryptStickyCookie(session)

	c.Check(host, Equals, nHost)
	c.Check(port, Equals, nPort)
}

func (s *StickySuite) TestDecryptionSessionExceedsLimit(c *C) {
	// Construct a session that exceeds 32 bytes
	host := "11111.11111.11111.11111.11111.11111"
	port := uint16(12345)
	session := s.se.encryptStickyCookie(host, port)

	nHost, nPort := s.se.decryptStickyCookie(session)

	c.Check(nHost, Equals, "")
	c.Check(nPort, Equals, uint16(0))
}

func (s *StickySuite) TestDecryptionInvalidSession(c *C) {
	session := "i am not a session"

	nHost, nPort := s.se.decryptStickyCookie(session)

	c.Check(nHost, Equals, "")
	c.Check(nPort, Equals, uint16(0))
}

func (s *StickySuite) TestDecryptionInvalidSessionButValidBase64(c *C) {
	session := base64.StdEncoding.EncodeToString([]byte("i am not a session"))

	nHost, nPort := s.se.decryptStickyCookie(session)

	c.Check(nHost, Equals, "")
	c.Check(nPort, Equals, uint16(0))
}
