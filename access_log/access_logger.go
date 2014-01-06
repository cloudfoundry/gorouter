package access_log

import (
	"github.com/cloudfoundry/gorouter/route"
	"net/http"
	"time"
)

type AccessLogRecord struct {
	Request       *http.Request
	Response      *http.Response
	RouteEndpoint *route.Endpoint
	StartedAt     time.Time
	FirstByteAt   time.Time
	FinishedAt    time.Time
	BodyBytesSent int64
}

type AccessLogger interface {
	Run()
	Stop()
	Log(record AccessLogRecord)
}
