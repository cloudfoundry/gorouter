package test_helpers

type StatusReporter struct {
	locked   chan bool
	reported chan bool
}

func NewStatusReporter(status <-chan bool) *StatusReporter {
	reporter := &StatusReporter{
		locked:   make(chan bool),
		reported: make(chan bool),
	}

	go reporter.collectUpdates(status)

	return reporter
}

func (reporter *StatusReporter) Locked() bool {
	return <-reporter.locked
}

func (reporter *StatusReporter) Reporting() bool {
	return <-reporter.reported
}

func (reporter *StatusReporter) collectUpdates(status <-chan bool) {
	locked := false
	reporting := false

	for {
		select {
		case locked, reporting = <-status:
			if !reporting {
				close(reporter.reported)
				close(reporter.locked)
				return
			}
		case reporter.reported <- reporting:
		case reporter.locked <- locked:
		}
	}
}
