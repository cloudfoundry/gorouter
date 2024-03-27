package handlers

import (
	"net/http"
	"strings"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
)

type HopByHop struct {
	cfg    *config.Config
	logger logger.Logger
}

// NewHopByHop creates a new handler that sanitizes hop-by-hop headers based on the HopByHopHeadersToFilter config
func NewHopByHop(cfg *config.Config, logger logger.Logger) *HopByHop {
	return &HopByHop{
		logger: logger,
		cfg:    cfg,
	}
}

func (h *HopByHop) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	h.SanitizeRequestConnection(r)
	next(rw, r)
}

func (h *HopByHop) SanitizeRequestConnection(r *http.Request) {
	if len(h.cfg.HopByHopHeadersToFilter) == 0 {
		return
	}
	connections := r.Header.Values("Connection")
	for index, connection := range connections {
		if connection != "" {
			values := strings.Split(connection, ",")
			connectionHeader := []string{}
			for i := range values {
				trimmedValue := strings.TrimSpace(values[i])
				found := false
				for _, item := range h.cfg.HopByHopHeadersToFilter {
					if strings.EqualFold(item, trimmedValue) {
						found = true
						break
					}
				}
				if !found {
					connectionHeader = append(connectionHeader, trimmedValue)
				}
			}
			r.Header[http.CanonicalHeaderKey("Connection")][index] = strings.Join(connectionHeader, ", ")
		}
	}
}
