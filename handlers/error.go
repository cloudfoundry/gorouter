package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
)

type ErrorWriter interface {
	WriteError(
		rw http.ResponseWriter,
		code int,
		message string,
		logger logger.Logger,
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
	logger logger.Logger,
) {
	body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), message)

	if code != http.StatusNotFound {
		logger.Info("status", zap.String("body", body))
	}

	if code > 299 {
		rw.Header().Del("Connection")
	}

	rw.WriteHeader(code)
	rw.Write([]byte(body))
}

type htmlErrorWriter struct {
	tpl *template.Template
}

func NewHTMLErrorWriter(text string) (ErrorWriter, error) {
	ew := &htmlErrorWriter{}

	tpl, err := template.New("error-message").Parse(text)
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
	logger logger.Logger,
) {
	body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), message)

	if code != http.StatusNotFound {
		logger.Info("status", zap.String("body", body))
	}

	if code > 299 {
		rw.Header().Del("Connection")
	}

	rw.WriteHeader(code)

	var rendered bytes.Buffer
	if err := ew.tpl.Execute(&rendered, nil); err != nil {
		logger.Error("render-error-failed", zap.Error(err))
		rw.Write([]byte(body))
		return
	}

	rw.Write(rendered.Bytes())
}
