package utils

import (
	"net/http"
)

type HeaderRewriter interface {
	RewriteHeader(http.Header)
}

// AddHeaderIfNotPresentRewriter: Adds headers only if they are not present
// in the current http.Header.
// The http.Header must be built using the method Add() to canonalize the keys
type AddHeaderIfNotPresentRewriter struct {
	Header http.Header
}

func (i *AddHeaderIfNotPresentRewriter) RewriteHeader(header http.Header) {
	for h, v := range i.Header {
		if _, ok := header[h]; !ok {
			header[h] = v
		}
	}
}

// RemoveHeaderRewriter: Removes any value associated to a header
// The http.Header must be built using the method Add() to canonalize the keys
type RemoveHeaderRewriter struct {
	Header http.Header
}

func (i *RemoveHeaderRewriter) RewriteHeader(header http.Header) {
	for h, _ := range i.Header {
		header.Del(h)
	}
}
