package token_fetcher

type NoOpTokenFetcher struct {
}

func NewNoOpTokenFetcher() *NoOpTokenFetcher {
	return &NoOpTokenFetcher{}
}

func (f *NoOpTokenFetcher) FetchToken() (*Token, error) {
	return &Token{}, nil
}
