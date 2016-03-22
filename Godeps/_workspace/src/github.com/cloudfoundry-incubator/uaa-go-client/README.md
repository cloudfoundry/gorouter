[![Build Status](https://travis-ci.org/cloudfoundry-incubator/uaa-go-client.svg?branch=master)](https://travis-ci.org/cloudfoundry-incubator/uaa-go-client)

# uaa-go-client
A go library for Cloud Foundry [UAA](https://github.com/cloudfoundry/uaa) that provides the following:
- fetch access tokens (including ability to cache tokens)
- decode tokens
- get token signing key


## Example
This example client connects to UAA using https and skips cert verification.
```go
cfg := &config.Config{
  ClientName:       "client-name",
	ClientSecret:     "client-secret",
	UaaEndpoint:      "https://uaa.service.cf.internal:8443",
	SkipVerification: true,
}

uaaClient, err = client.NewClient(logger, cfg, clock)
if err != nil {
  log.Fatal(err)
  os.Exit(1)
}

fmt.Printf("Connecting to: %s ...\n", cfg.UaaEndpoint)

token, err = uaaClient.FetchToken(true)
if err != nil {
  log.Fatal(err)
  os.Exit(1)
}

fmt.Printf("Token: %#v\n", token)
```

## Example command line clients
The following example clients can be used to fetch a token or verification key from UAA in a local BOSH Lite deployment.

### Prerequisites for testing these example clients with BOSH Lite

- Add IP of UAA your /etc/hosts (can be found using `bosh vms`)

		10.244.0.134 uaa.service.cf.internal

- In your deployment manifest for cf-release configure UAA to listen on TLS by specifying the port, certificate, and key with the following properties:

		properties:
		  uaa:
		    ssl:
		      port: 8443
		    sslCertificate: |
		      -----BEGIN CERTIFICATE-----
		      MIIDAjCCAmugAwIBAgIJAJtrcBsKNfWDMA0GCSqGSIb3DQEBCwUAMIGZMQswCQYD
		      VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTEWMBQGA1UEBwwNU2FuIEZyYW5j
		      aXNjbzEQMA4GA1UECgwHUGl2b3RhbDERMA8GA1UECwwISWRlbnRpdHkxFjAUBgNV
		      BAMMDU1hcmlzc2EgS29hbGExIDAeBgkqhkiG9w0BCQEWEW1rb2FsYUBwaXZvdGFs
		      LmlvMB4XDTE1MDczMDE5Mzk0NVoXDTI1MDcyOTE5Mzk0NVowgZkxCzAJBgNVBAYT
		      AlVTMRMwEQYDVQQIDApDYWxpZm9ybmlhMRYwFAYDVQQHDA1TYW4gRnJhbmNpc2Nv
		      MRAwDgYDVQQKDAdQaXZvdGFsMREwDwYDVQQLDAhJZGVudGl0eTEWMBQGA1UEAwwN
		      TWFyaXNzYSBLb2FsYTEgMB4GCSqGSIb3DQEJARYRbWtvYWxhQHBpdm90YWwuaW8w
		      gZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJAoGBAPVOIGvG8MFbkqi+ytdBHVbEGde4
		      jaCphmvGm89/4Ks0r+041VsS55XNYnHsxXTlh1FiB2KcbrDb33pgvuAIYpcAO2I0
		      gqGeRoS2hNsxzcFdkgSZn1umDAeoE4bCATrquN93KMcw/coY5jacUfb9P2CQztkS
		      e2o+QWtIaWYAvI3bAgMBAAGjUDBOMB0GA1UdDgQWBBTkEjA4CEjevAGfnPBciyXC
		      3v4zMzAfBgNVHSMEGDAWgBTkEjA4CEjevAGfnPBciyXC3v4zMzAMBgNVHRMEBTAD
		      AQH/MA0GCSqGSIb3DQEBCwUAA4GBAIEd8U32tkcvwG9qCOfe5raBENHM4ltTuhju
		      zZWIM5Ik1bFf6+rA71HVDD1Z5fRozidhMOl6mrrGShfu6VUjtqzctJeSjaOPIJL+
		      wvrXXcAkCYZ9QKf0sqlUWcIRy90nqrD5sL/rHAjNjxQ3lqIOj7yWOgty4LUzFQNr
		      FHiyd3T6
		      -----END CERTIFICATE-----
		    sslPrivateKey: |
		      -----BEGIN RSA PRIVATE KEY-----
		      MIICXwIBAAKBgQD1TiBrxvDBW5KovsrXQR1WxBnXuI2gqYZrxpvPf+CrNK/tONVb
		      EueVzWJx7MV05YdRYgdinG6w2996YL7gCGKXADtiNIKhnkaEtoTbMc3BXZIEmZ9b
		      pgwHqBOGwgE66rjfdyjHMP3KGOY2nFH2/T9gkM7ZEntqPkFrSGlmALyN2wIDAQAB
		      AoGBAPBvfz+kYt5iz0EuoMqTPBqLY3kZn1fWUbbZmGatxJyKq9UsW5NE2FDwWomn
		      tXJ6d0PBfdOd2LDpEgZ1RSF5lobXn2m2+YeEso7A7yMiBRW8CIrkUn8wVA0s42t+
		      osElfvj73G2ZjCqQm6BLCjtFYnalmZIzfOCB26xRWaf0MJ7hAkEA/XaqnosJfmRp
		      kmvto81LEvjVVlSvpo+6rt66ykywEv9daHWZZBrrwVz3Iu4oXlwPuF8bcO8JMLRf
		      OH98T1+1PQJBAPfCj0r3fRhmBZMWqf2/tbeQPvIQzqSXfYroFgnKIKxVCV8Bkm3q
		      1rP4c0XDHEWYIwvMWBTOmVSZqfSxtwIicPcCQQDCcRqK7damo5lpvmpb0s3ZDBN9
		      WxI1EOYB6NQbBaG9sTGTRUQbS5u4hv0ASvulB7L3md6PUJEYUAcMbKCMs7txAkEA
		      7C8pwHJba0XebJB/bqkxxpKYntPM2fScNi32zFBGg2HxNANgnq3vDNN8t/U+X02f
		      oyCimvs0CgUOknhTmJJSkwJBAPaI298JxTnWncC3Zu7d5QYCJXjU403Aj4LdcVeI
		      6A15MzQdj5Hm82vlmpC4LzXofLjiN4E5ZLluzEw+1TjRE7c=
		      -----END RSA PRIVATE KEY-----


- Assuming the cert you've configured for UAA is self-signed, provide `true` for the `skip-verification` option

### Fetch token
This client connects to UAA using https and fetches a token.

```
Usage: <client-name> <client-secret> <uaa-url> <skip-verification>
```

Example
```
$ go run examples/fetch_token.go gorouter gorouter-secret https://uaa.service.cf.internal:8443 true

Connecting to: https://uaa.service.cf.internal:8443 ...
Response:
	token: eyJhbGciOiJSUzI1NiJ9.eyJqdGkiOiJlOGQ3NWJiNi1kMGMxLTRmMjEtYWMyMy05ZGRiNmY2MWI3ZjkiLCJzdWIiOiJnb3JvdXRlciIsImF1dGhvcml0aWVzIjpbInJvdXRpbmcucm91dGVzLnJlYWQiXSwic2NvcGUiOlsicm91dGluZy5yb3V0ZXMucmVhZCJdLCJjbGllbnRfaWQiOiJnb3JvdXRlciIsImNpZCI6Imdvcm91dGVyIiwiYXpwIjoiZ29yb3V0ZXIiLCJncmFudF90eXBlIjoiY2xpZW50X2NyZWRlbnRpYWxzIiwicmV2X3NpZyI6IjdmNTE1MmQyIiwiaWF0IjoxNDU0NzA5NTUxLCJleHAiOjE0NTQ3NTI3NTEsImlzcyI6Imh0dHBzOi8vdWFhLmJvc2gtbGl0ZS5jb20vb2F1dGgvdG9rZW4iLCJ6aWQiOiJ1YWEiLCJhdWQiOlsiZ29yb3V0ZXIiLCJyb3V0aW5nLnJvdXRlcyJdfQ.QSdLbdhDFWQXSJ3lPbTVUCj6zEH1DUPU3V-x8lX48qOPg99snalEEIBX5y5Ki6mZLWJ9p6UUIH1xANz4mGATcBIO282wcRBK0Pbc-r1OkjFNJTvwdV75kP9ovbGXGNbQZMksEvEtgOQ_icz7XsJrkTxtV29uPYDpKHbxtvqpPeU
	expires: 43199
```

### Fetch key
This client connects to UAA using https and fetches the UAA verification key. An Oauth client is not required as the target API endpoint on UAA does not require authentication.

```
Usage: <uaa-url> <skip-verification>
```

Example
```
$ go run examples/fetch_key.go https://uaa.service.cf.internal:8443 true

Connecting to: https://uaa.service.cf.internal:8443 ...
Response:
	token: eyJhbGciOiJSUzI1NiJ9.eyJqdGkiOiJlOGQ3NWJiNi1kMGMxLTRmMjEtYWMyMy05ZGRiNmY2MWI3ZjkiLCJzdWIiOiJnb3JvdXRlciIsImF1dGhvcml0aWVzIjpbInJvdXRpbmcucm91dGVzLnJlYWQiXSwic2NvcGUiOlsicm91dGluZy5yb3V0ZXMucmVhZCJdLCJjbGllbnRfaWQiOiJnb3JvdXRlciIsImNpZCI6Imdvcm91dGVyIiwiYXpwIjoiZ29yb3V0ZXIiLCJncmFudF90eXBlIjoiY2xpZW50X2NyZWRlbnRpYWxzIiwicmV2X3NpZyI6IjdmNTE1MmQyIiwiaWF0IjoxNDU0NzA5NTUxLCJleHAiOjE0NTQ3NTI3NTEsImlzcyI6Imh0dHBzOi8vdWFhLmJvc2gtbGl0ZS5jb20vb2F1dGgvdG9rZW4iLCJ6aWQiOiJ1YWEiLCJhdWQiOlsiZ29yb3V0ZXIiLCJyb3V0aW5nLnJvdXRlcyJdfQ.QSdLbdhDFWQXSJ3lPbTVUCj6zEH1DUPU3V-x8lX48qOPg99snalEEIBX5y5Ki6mZLWJ9p6UUIH1xANz4mGATcBIO282wcRBK0Pbc-r1OkjFNJTvwdV75kP9ovbGXGNbQZMksEvEtgOQ_icz7XsJrkTxtV29uPYDpKHbxtvqpPeU
	expires: 43199
```
