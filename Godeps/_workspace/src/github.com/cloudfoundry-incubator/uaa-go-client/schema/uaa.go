package schema

type Token struct {
	AccessToken string `json:"access_token"`
	// Expire time in seconds
	ExpiresIn int64 `json:"expires_in"`
}

type UaaKey struct {
	Alg   string `json:"alg"`
	Value string `json:"value"`
}
