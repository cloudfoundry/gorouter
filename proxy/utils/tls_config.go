package utils

import "crypto/tls"

func TLSConfigWithServerName(newServerName string, template *tls.Config) *tls.Config {
	return &tls.Config{
		CipherSuites:       template.CipherSuites,
		InsecureSkipVerify: template.InsecureSkipVerify,
		RootCAs:            template.RootCAs,
		ServerName:         newServerName,
		Certificates:       template.Certificates,
	}
}
