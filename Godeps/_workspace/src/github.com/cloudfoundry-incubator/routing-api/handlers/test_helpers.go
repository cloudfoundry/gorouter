package handlers

import (
	"bytes"
	"encoding/json"
	. "github.com/onsi/gomega"
	"io"
	"net/http"
	"strings"
)

func NewTestRequest(body interface{}) *http.Request {
	var reader io.Reader
	switch body := body.(type) {

	case string:
		reader = strings.NewReader(body)
	case []byte:
		reader = bytes.NewReader(body)
	default:
		jsonBytes, err := json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())
		reader = bytes.NewReader(jsonBytes)
	}

	request, err := http.NewRequest("", "", reader)
	Expect(err).ToNot(HaveOccurred())
	return request
}
