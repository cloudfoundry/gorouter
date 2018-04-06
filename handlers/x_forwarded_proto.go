package handlers

import "net/http"

type XForwardedProto struct {
	SkipSanitization         func(req *http.Request) bool
	ForceForwardedProtoHttps bool
	SanitizeForwardedProto   bool
}

func (h *XForwardedProto) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	newReq := new(http.Request)
	*newReq = *r
	if !h.SkipSanitization(r) {
		if h.ForceForwardedProtoHttps {
			newReq.Header.Set("X-Forwarded-Proto", "https")
		} else if h.SanitizeForwardedProto || newReq.Header.Get("X-Forwarded-Proto") == "" {
			scheme := "http"
			if newReq.TLS != nil {
				scheme = "https"
			}
			newReq.Header.Set("X-Forwarded-Proto", scheme)
		}
	}
	next(rw, newReq)
}
