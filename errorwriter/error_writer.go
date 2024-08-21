package errorwriter

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"

	log "code.cloudfoundry.org/gorouter/logger"
)

type ErrorWriter interface {
	WriteError(
		rw http.ResponseWriter,
		code int,
		message string,
		logger *slog.Logger,
	)
}

type plaintextErrorWriter struct{}

func NewPlaintextErrorWriter() ErrorWriter {
	return &plaintextErrorWriter{}
}

// WriteStatus attempts to template an error message.
func (ew *plaintextErrorWriter) WriteError(
	rw http.ResponseWriter,
	code int,
	message string,
	logger *slog.Logger,
) {
	body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), message)

	if code != http.StatusNotFound {
		logger.Info("status", slog.String("body", body))
	}

	if code > 299 {
		rw.Header().Del("Connection")
	}

	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.Header().Set("X-Content-Type-Options", "nosniff")

	rw.WriteHeader(code)
	fmt.Fprintln(rw, body)
}

type htmlErrorWriter struct {
	tpl *template.Template
}

type htmlErrorWriterContext struct {
	Status     int
	StatusText string
	Message    string
	Header     http.Header
}

func NewHTMLErrorWriterFromFile(path string) (ErrorWriter, error) {
	ew := &htmlErrorWriter{}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Could not read HTML error template file: %s", err)
	}

	tpl, err := template.New("error-message").Parse(string(bytes))
	if err != nil {
		return nil, err
	}
	ew.tpl = tpl

	return ew, nil
}

// WriteStatus attempts to template an error message.
// If the template cannot be rendered then text will be sent instead
// and the error will be returned even though the response has been sent
func (ew *htmlErrorWriter) WriteError(
	rw http.ResponseWriter,
	code int,
	message string,
	logger *slog.Logger,
) {
	body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), message)

	if code != http.StatusNotFound {
		logger.Info("status", slog.String("body", body))
	}

	if code > 299 {
		rw.Header().Del("Connection")
	}

	tplContext := htmlErrorWriterContext{
		Status:     code,
		StatusText: http.StatusText(code),
		Message:    message,
		Header:     rw.Header(),
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")

	var respBytes []byte
	var rendered bytes.Buffer
	if err := ew.tpl.Execute(&rendered, &tplContext); err != nil {
		logger.Error("render-error-failed", log.ErrAttr(err))
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		respBytes = []byte(body)
	} else {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		respBytes = rendered.Bytes()
	}

	rw.WriteHeader(code)
	rw.Write(respBytes)
}
