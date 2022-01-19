package utils

import "crypto/tls"

func TLSConfigWithServerName(newServerName string, template *tls.Config, isRouteService bool) *tls.Config {
	config := &tls.Config{
		CipherSuites:       template.CipherSuites,
		InsecureSkipVerify: template.InsecureSkipVerify,
		RootCAs:            template.RootCAs,
		ServerName:         newServerName,
		Certificates:       template.Certificates,
	}

	if isRouteService {
		config.MinVersion = template.MinVersion
		config.MaxVersion = template.MaxVersion
	}
	return config
}
