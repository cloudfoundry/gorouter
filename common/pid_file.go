package common

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

type PidFile struct {
	file string
}

func NewPidFile(filename string) (*PidFile, error) {
	pidFile := new(PidFile)
	pidFile.file = filename

	if err := pidFile.writePid(); err != nil {
		return nil, err
	}

	return pidFile, nil
}

func (p *PidFile) writePid() error {
	file, err := os.OpenFile(p.file, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)

	var pid int = os.Getpid()
	var rpid int

	rpid, err = readPid(file)
	if err != nil {
		goto writepid
	}

	if rpid == pid {
		return nil
	} else if ProcessExist(rpid) {
		return errors.New(fmt.Sprintf("process(%d) already exists!", rpid))
	}

writepid:
	if err := file.Truncate(0); err != nil {
		return err
	}

	var spid string = strconv.Itoa(pid)
	var n int
	n, err = file.WriteString(spid)
	if err != nil || n != len(spid) {
		return err
	}

	return nil
}

func (p *PidFile) Unlink() {
	os.Remove(p.file)
}

func (p *PidFile) UnlinkOnSignal(sig ...os.Signal) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, sig...)

	// FIXME: can register multiple handler? or atexit method?
	go func() {
		for _ = range c {
			p.Unlink()
			os.Exit(0)
		}
	}()
}

func readPid(file *os.File) (int, error) {
	var buf [8]byte

	n, err := file.Read(buf[:])
	if err != nil {
		return 0, err
	}

	str := strings.Trim(string(buf[:n]), " \n\t")

	return strconv.Atoi(str)
}
