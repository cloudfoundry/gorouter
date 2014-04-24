package cmdtest

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/onsi/gomega/gexec"
)

type Session struct {
	gsess *gexec.Session

	Cmd *exec.Cmd

	Stdin io.WriteCloser

	stdout *Expector
	stderr *Expector

	exited chan int
}

type OutputWrapper func(io.Writer) io.Writer

func Start(cmd *exec.Cmd) (*Session, error) {
	return StartWrapped(cmd, noopWrapper, noopWrapper)
}

func StartWrapped(cmd *exec.Cmd, outWrapper OutputWrapper, errWrapper OutputWrapper) (*Session, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	gsess, err := gexec.Start(cmd, outWrapper(nil), errWrapper(nil))
	if err != nil {
		return nil, err
	}

	outExpector := NewBufferExpector(gsess.Out, 0)
	errExpector := NewBufferExpector(gsess.Err, 0)

	return &Session{
		Cmd:   cmd,
		Stdin: stdin,

		gsess:  gsess,
		stdout: outExpector,
		stderr: errExpector,
	}, nil
}

func (s Session) ExpectOutput(pattern string) error {
	return s.stdout.Expect(pattern)
}

func (s Session) ExpectOutputBranches(branches ...ExpectBranch) error {
	return s.stdout.ExpectBranches(branches...)
}

func (s Session) ExpectOutputWithTimeout(pattern string, timeout time.Duration) error {
	return s.stdout.ExpectWithTimeout(pattern, timeout)
}

func (s Session) ExpectError(pattern string) error {
	return s.stderr.Expect(pattern)
}

func (s Session) ExpectErrorWithTimeout(pattern string, timeout time.Duration) error {
	return s.stderr.ExpectWithTimeout(pattern, timeout)
}

func (s Session) Wait(timeout time.Duration) (int, error) {
	tick := 100 * time.Millisecond

	for i := time.Duration(0); i < timeout; i += tick {
		exitCode := s.gsess.ExitCode()

		if exitCode != -1 {
			return exitCode, nil
		}

		time.Sleep(tick)
	}

	return -1, fmt.Errorf("command did not exit: %s", strings.Join(s.Cmd.Args, " "))
}

func (s Session) FullOutput() []byte {
	return s.stdout.FullOutput()
}

func (s Session) FullErrorOutput() []byte {
	return s.stderr.FullOutput()
}

func noopWrapper(out io.Writer) io.Writer {
	return out
}
