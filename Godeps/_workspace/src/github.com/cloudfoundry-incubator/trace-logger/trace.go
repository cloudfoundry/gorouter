package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"time"
)

type Printer interface {
	Print(v ...interface{})
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

type nullLogger struct{}

func (*nullLogger) Print(v ...interface{})                 {}
func (*nullLogger) Printf(format string, v ...interface{}) {}
func (*nullLogger) Println(v ...interface{})               {}

var stdOut io.Writer = os.Stdout
var Logger Printer

func init() {
	Logger = NewLogger("")
}

func SetStdout(s io.Writer) {
	stdOut = s
}

func NewLogger(env_setting string) Printer {
	if env_setting == "true" {
		Logger = newStdoutLogger()
	} else {
		Logger = new(nullLogger)
	}

	return Logger
}

func newStdoutLogger() Printer {
	return log.New(stdOut, "", 0)
}

func Sanitize(input string) (sanitized string) {
	var sanitizeJson = func(propertyName string, json string) string {
		regex := regexp.MustCompile(fmt.Sprintf(`"%s":\s*"[^"]*"`, propertyName))
		return regex.ReplaceAllString(json, fmt.Sprintf(`"%s":"%s"`, propertyName, PRIVATE_DATA_PLACEHOLDER()))
	}

	re := regexp.MustCompile(`(?m)^Authorization: .*`)
	sanitized = re.ReplaceAllString(input, "Authorization: "+PRIVATE_DATA_PLACEHOLDER())
	re = regexp.MustCompile(`password=[^&]*&`)
	sanitized = re.ReplaceAllString(sanitized, "password="+PRIVATE_DATA_PLACEHOLDER()+"&")

	sanitized = sanitizeJson("access_token", sanitized)
	sanitized = sanitizeJson("refresh_token", sanitized)
	sanitized = sanitizeJson("token", sanitized)
	sanitized = sanitizeJson("password", sanitized)
	sanitized = sanitizeJson("oldPassword", sanitized)

	return
}

func PRIVATE_DATA_PLACEHOLDER() string {
	return "[PRIVATE DATA HIDDEN]"
}

func DumpRequest(req *http.Request) {
	dumpedRequest, err := httputil.DumpRequest(req, true)
	if err != nil {
		Logger.Printf("Error dumping request\n%s\n", err)
	} else {
		Logger.Printf("\n%s [%s]\n%s\n", "REQUEST:", time.Now().Format(time.RFC3339), Sanitize(string(dumpedRequest)))
	}
}

func DumpResponse(resp *http.Response) {
	dumpedResponse, err := httputil.DumpResponse(resp, true)
	if err != nil {
		Logger.Printf("Error dumping response\n%s\n", err)
	} else {
		Logger.Printf("\n%s [%s]\n%s\n", "RESPONSE:", time.Now().Format(time.RFC3339), Sanitize(string(dumpedResponse)))
	}
}

func DumpJSON(label string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		Logger.Printf("Error dumping json object\n%s\n", err)
	} else {
		Logger.Printf("\n%s [%s]\n%s\n", label+":", time.Now().Format(time.RFC3339), Sanitize(string(jsonData)))
	}
}
