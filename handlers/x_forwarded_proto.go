package handlers

import (
	"net/http"
	"runtime/trace"
)

type XForwardedProto struct {
	SkipSanitization         func(req *http.Request) bool
	ForceForwardedProtoHttps bool
	SanitizeForwardedProto   bool
}

func (h *XForwardedProto) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	defer trace.StartRegion(r.Context(), "XForwardedProto.ServeHTTP").End()

	newReq := new(http.Request)
	*newReq = *r
	skip := h.SkipSanitization(r)
	if !skip {
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
