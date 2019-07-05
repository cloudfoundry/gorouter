package test_util

import (
	"fmt"
	"github.com/cloudfoundry/custom-cats-reporters/honeycomb/client"
	"github.com/onsi/ginkgo/config"
	"github.com/onsi/ginkgo/types"
	"strings"
)

type FailureReporter struct {
	client client.Client
}

func NewFailureReporter(client client.Client) FailureReporter {
	return FailureReporter{
		client: client,
	}
}

func (fr FailureReporter) SpecDidComplete(ss *types.SpecSummary) {
	if ss.HasFailureState() {
		_ = fr.client.SendEvent(
			map[string]string{
				"State":                 getTestState(ss.State),
				"Description":           strings.Join(ss.ComponentTexts, " | "),
				"FailureMessage":        ss.Failure.Message,
				"FailureLocation":       ss.Failure.Location.String(),
				"FailureOutput":         ss.CapturedOutput,
				"ComponentCodeLocation": ss.Failure.ComponentCodeLocation.String(),
				"RunTimeInSeconds":      fmt.Sprintf("%f", ss.RunTime.Seconds()),
			},
			map[string]string{},
			map[string]string{},
		)
	}
}

func (fr FailureReporter) SpecSuiteWillBegin(config config.GinkgoConfigType, summary *types.SuiteSummary) {
}
func (fr FailureReporter) BeforeSuiteDidRun(setupSummary *types.SetupSummary) {}
func (fr FailureReporter) SpecWillRun(specSummary *types.SpecSummary)         {}
func (fr FailureReporter) AfterSuiteDidRun(setupSummary *types.SetupSummary)  {}
func (fr FailureReporter) SpecSuiteDidEnd(summary *types.SuiteSummary)        {}

func getTestState(state types.SpecState) string {
	switch state {
	case types.SpecStatePassed:
		return "passed"
	case types.SpecStateFailed:
		return "failed"
	case types.SpecStatePending:
		return "pending"
	case types.SpecStateSkipped:
		return "skipped"
	case types.SpecStatePanicked:
		return "panicked"
	case types.SpecStateTimedOut:
		return "timedOut"
	case types.SpecStateInvalid:
		return "invalid"
	default:
		panic("unknown spec state")
	}
}
