package spec

import (
	"os"
	"os/exec"
	"strconv"
	"sync"
)

type NatsServer struct {
	sync.RWMutex

	port    uint16
	pidfile string
	process *os.Process
	status  error
}

// TODO: add functions like WaitForRunning, Status

func NewNatsServer(port uint16, pidfile string) *NatsServer {
	s := new(NatsServer)

	s.port = port
	s.pidfile = pidfile

	return s
}

func (s *NatsServer) Start() error {
	s.Lock()
	defer s.Unlock()

	cmd := exec.Command("nats-server", "-p", strconv.Itoa(int(s.port)), "-P", s.pidfile,
		"-V", "-D")

	err := cmd.Start()
	if err != nil {
		return err
	}
	s.process = cmd.Process

	go func() {
		err = cmd.Wait()

		s.Lock()
		defer s.Unlock()

		s.process = nil
		s.status = err
	}()

	return nil
}

func (s *NatsServer) Running() bool {
	s.RLock()
	defer s.RUnlock()

	return s.process != nil
}

func (s *NatsServer) Stop() {
	s.RLock()
	defer s.RUnlock()

	if !s.Running() {
		return
	}

	s.process.Kill()
}

func (s *NatsServer) Pid() int {
	s.RLock()
	defer s.RUnlock()

	if !s.Running() {
		return 0
	}

	return s.process.Pid
}
