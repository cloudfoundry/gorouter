package test_util

import (
	"github.com/cloudfoundry/custom-cats-reporters/honeycomb/client"
	"github.com/honeycombio/libhoney-go"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
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
