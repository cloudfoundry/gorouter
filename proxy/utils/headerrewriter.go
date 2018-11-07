package utils

import (
	"net/http"
)

type HeaderRewriter interface {
	RewriteHeader(http.Header)
}

// InjectHeaderRewriter: Adds headers only if they are not present in the current http.Header
type InjectHeaderRewriter struct {
	Header http.Header
}

func (i *InjectHeaderRewriter) RewriteHeader(header http.Header) {
	for h, v := range i.Header {
		if _, ok := header[h]; !ok {
			header[h] = v
		}
	}
}
