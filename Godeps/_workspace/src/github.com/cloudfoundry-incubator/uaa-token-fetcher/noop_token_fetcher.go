package token_fetcher

type NoOpTokenFetcher struct {
}

func NewNoOpTokenFetcher() TokenFetcher {
	return &NoOpTokenFetcher{}
}

func (f *NoOpTokenFetcher) FetchToken(useCachedToken bool) (*Token, error) {
	return &Token{}, nil
}
