package cmdtest

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/onsi/gomega/gbytes"
)

type Expector struct {
	output         io.Reader
	defaultTimeout time.Duration

	offset int
	buffer *gbytes.Buffer

	closed chan struct{}

	sync.RWMutex
}

type ExpectBranch struct {
	Pattern  string
	Callback func()
}

type ExpectationFailed struct {
	Branches []ExpectBranch
	Output   string
}

func (e ExpectationFailed) Error() string {
	patterns := []string{}

	for _, branch := range e.Branches {
		patterns = append(patterns, branch.Pattern)
	}

	return fmt.Sprintf(
		"Expected to see '%s'.\n\nFull output:\n\n%s",
		strings.Join(patterns, "' or '"),
		e.Output,
	)
}

func NewExpector(in io.Reader, defaultTimeout time.Duration) *Expector {
	buffer := gbytes.NewBuffer()

	go func() {
		io.Copy(buffer, in)
		buffer.Close()
	}()

	return NewBufferExpector(buffer, defaultTimeout)
}

func NewBufferExpector(buffer *gbytes.Buffer, defaultTimeout time.Duration) *Expector {
	closed := make(chan struct{})

	go func() {
		for {
			if buffer.Closed() {
				close(closed)
				break
			}

			time.Sleep(100 * time.Millisecond)
		}
	}()

	return &Expector{
		defaultTimeout: defaultTimeout,

		buffer: buffer,

		closed: closed,
	}
}

func (e *Expector) Expect(pattern string) error {
	return e.ExpectWithTimeout(pattern, e.defaultTimeout)
}

func (e *Expector) ExpectWithTimeout(pattern string, timeout time.Duration) error {
	return e.ExpectBranchesWithTimeout(
		timeout,
		ExpectBranch{
			Pattern:  pattern,
			Callback: func() {},
		},
	)
}

func (e *Expector) ExpectBranches(branches ...ExpectBranch) error {
	return e.ExpectBranchesWithTimeout(e.defaultTimeout, branches...)
}

func (e *Expector) ExpectBranchesWithTimeout(timeout time.Duration, branches ...ExpectBranch) error {
	matchResults := make(chan func(), len(branches))

	for _, branch := range branches {
		go e.match(matchResults, branch.Pattern, branch.Callback)
	}

	matchedCallback := make(chan func())
	allComplete := make(chan bool)

	go func() {
		for _ = range branches {
			result := <-matchResults

			if result != nil {
				matchedCallback <- result
			}
		}

		allComplete <- true
	}()

	timeoutChan := make(<-chan time.Time)

	if timeout != 0 {
		timeoutChan = time.After(timeout)
	}

	select {
	case callback := <-matchedCallback:
		callback()
		return nil
	case <-allComplete:
		return e.failedMatch(branches)
	case <-timeoutChan:
		e.buffer.CancelDetects()
		return e.failedMatch(branches)
	}
}

func (e *Expector) FullOutput() []byte {
	for {
		if e.buffer.Closed() {
			return e.buffer.Contents()
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func (e *Expector) match(result chan func(), pattern string, callback func()) {
	matched := e.matchOutput(pattern)

	if matched {
		result <- callback
	} else {
		result <- nil
	}
}

func (e *Expector) matchOutput(pattern string) bool {
	select {
	case val := <-e.buffer.Detect(pattern):
		return val
	case <-e.closed:
		ok, err := gbytes.Say(pattern).Match(e.buffer)
		return ok && err == nil
	}
}

func (e *Expector) failedMatch(branches []ExpectBranch) ExpectationFailed {
	return ExpectationFailed{
		Branches: branches,
		Output:   string(e.buffer.Contents()),
	}
}
