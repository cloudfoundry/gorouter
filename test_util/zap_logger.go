package test_util

import (
	"fmt"
	"regexp"
	"strings"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega/gbytes"
	"github.com/uber-go/zap"
)

// We add 1 to zap's default values to match our level definitions
// https://github.com/uber-go/zap/blob/47f41350ff078ea1415b63c117bf1475b7bbe72c/level.go#L36
func levelNumber(level zap.Level) int {
	return int(level) + 1
}

// TestZapLogger implements a zap logger that can be used with Ginkgo tests
type TestZapLogger struct {
	logger.Logger
	*TestZapSink
}

// Taken from github.com/uber-go/zap
type TestZapSink struct {
	*gbytes.Buffer
}

// NewTestZapLogger returns a new test logger using zap
func NewTestZapLogger(component string) *TestZapLogger {
	sink := &TestZapSink{
		Buffer: gbytes.NewBuffer(),
	}
	testLogger := logger.NewLogger(
		component,
		"unix-epoch",
		zap.DebugLevel,
		zap.Output(zap.MultiWriteSyncer(sink, zap.AddSync(ginkgo.GinkgoWriter))),
		zap.ErrorOutput(zap.MultiWriteSyncer(sink, zap.AddSync(ginkgo.GinkgoWriter))),
	)
	return &TestZapLogger{
		Logger:      testLogger,
		TestZapSink: sink,
	}
}

func (s *TestZapSink) Sync() error {
	return nil
}

func (s *TestZapSink) Lines() []string {
	output := strings.Split(string(s.Contents()), "\n")
	return output[:len(output)-1]
}

// Buffer returns the gbytes buffer that was used as the sink
func (z *TestZapLogger) Buffer() *gbytes.Buffer {
	return z.TestZapSink.Buffer
}

func (z *TestZapLogger) Lines(level zap.Level) []string {
	r, _ := regexp.Compile(fmt.Sprintf(".*\"log_level\":%d.*}\n", levelNumber(level)))
	return r.FindAllString(string(z.TestZapSink.Buffer.Contents()), -1)
}
