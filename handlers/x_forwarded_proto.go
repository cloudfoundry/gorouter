package handlers

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
)

type XForwardedProto struct {
	SkipSanitization         func(req *http.Request) (bool, error)
	ForceForwardedProtoHttps bool
	SanitizeForwardedProto   bool
	Logger                   logger.Logger
}

func (h *XForwardedProto) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	newReq := new(http.Request)
	*newReq = *r
	skip, err := h.SkipSanitization(r)
	if err != nil {
		h.Logger.Error("signature-validation-failed", zap.Error(err))
		writeStatus(
			rw,
			http.StatusBadRequest,
			"Failed to validate Route Service Signature for x-forwarded-proto",
			h.Logger,
		)
		return
	}
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
