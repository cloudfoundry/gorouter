package test_util

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"

	log "code.cloudfoundry.org/gorouter/logger"
)

// We add 1 to zap's default values to match our level definitions
// https://github.com/uber-go/zap/blob/47f41350ff078ea1415b63c117bf1475b7bbe72c/level.go#L36
func levelNumber(level zapcore.Level) int {
	return int(level) + 1
}

// TestLogger implements a zap logger that can be used with Ginkgo tests
type TestLogger struct {
	*slog.Logger
	*TestSink
}

// Taken from go.uber.org/zap
type TestSink struct {
	*gbytes.Buffer
}

// NewTestLogger returns a new slog logger using a zap handler
func NewTestLogger(component string) *TestLogger {
	sink := &TestSink{
		Buffer: gbytes.NewBuffer(),
	}
	var testLogger *slog.Logger
	if component != "" {
		testLogger = log.CreateLoggerWithSource(component, "")
	} else {
		testLogger = log.CreateLogger()
	}

	log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(sink, zapcore.AddSync(ginkgo.GinkgoWriter)))
	log.SetLoggingLevel("Debug")
	return &TestLogger{
		Logger:   testLogger,
		TestSink: sink,
	}
}

func (s *TestSink) Sync() error {
	return nil
}

func (s *TestSink) Lines() []string {
	output := strings.Split(string(s.Contents()), "\n")
	return output[:len(output)-1]
}

// Buffer returns the gbytes buffer that was used as the sink
func (z *TestLogger) Buffer() *gbytes.Buffer {
	return z.TestSink.Buffer
}

func (z *TestLogger) Lines(level zapcore.Level) []string {
	r, _ := regexp.Compile(fmt.Sprintf(".*\"log_level\":%d.*}\n", levelNumber(level)))
	return r.FindAllString(string(z.TestSink.Buffer.Contents()), -1)
}
