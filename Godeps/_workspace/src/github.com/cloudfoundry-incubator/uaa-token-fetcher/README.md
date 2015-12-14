#CF UAA Token Fetcher
A go library for getting oauth tokens (client credentials flow) from CF UAA.

1. It caches access token until its expiration and returns the cached token unless otherwise specified
1. It retries multiple times fresh token when either
	1. UAA is unreachable
	1. UAA returns 5xx status code

-------

Usage:

- Create the OAuth parameters:
```go
type OAuthConfig struct {
	TokenEndpoint string `yaml:"token_endpoint"`
	ClientName    string `yaml:"client_name"`
	ClientSecret  string `yaml:"client_secret"`
	Port          int    `yaml:"port"`
}
```
- Create the caching and retry configuration:
```go
type TokenFetcherConfig struct {
	MaxNumberOfRetries   uint32
	RetryInterval        time.Duration
	ExpirationBufferTime int64
}
```
- Invoke the `NewTokenFetcher`, passing the required parameters:
This pointer is passed into the `NewTokenFetcher` function
```go
func NewTokenFetcher(
	logger lager.Logger,
	config *OAuthConfig,
	tokenFetcherConfig TokenFetcherConfig,
	clock clock.Clock) (TokenFetcher, error)
```
- To use the `TokenFetcher`, simply call `FetchToken`, indicating if it can use a previously cached token or it should fetch a new token from UAA.
```go
token, err := fetcher.FetchToken(true)
```
the `Token` has an `AccessTime string` and an `ExpireTime int`.
