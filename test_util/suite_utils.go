package test_util

import (
	"os"
	"testing"

	"github.com/cloudfoundry/custom-cats-reporters/honeycomb/client"
	"github.com/honeycombio/libhoney-go"

	. "github.com/onsi/ginkgo/v2"
)

func RunSpecWithHoneyCombReporter(t *testing.T, desc string) {
	honeyCombClient := client.New(libhoney.Config{
		WriteKey: os.Getenv("HONEYCOMB_KEY"),
		Dataset:  "gorouter",
	})

	RunSpecsWithDefaultAndCustomReporters(t, desc, []Reporter{
		NewFailureReporter(honeyCombClient),
	})
}
