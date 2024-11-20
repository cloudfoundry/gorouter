// This file is part of gorouter of Cloud Foundry. The implementation is a modified version of the
// Go standard library implementation at log/syslog/syslog.go. Any modifications are licensed under
// the license of gorouter which can be found in the LICENSE file.
//
// Original License:
//
// Copyright 2009 The Go Authors.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google LLC nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

// Package syslog implements a syslog writer over UDP and TCP following RFC5424, RFC5426 and
// RFC6587. It is designed to serve as an access log writer for gorouter and is therefore not
// general purpose.
package syslog

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ByteOrderMark as required by RFC5424
const ByteOrderMark = "\ufeff"

// The Priority is a combination of the syslog facility and
// severity. For example, [SeverityAlert] | [FacilityFtp] sends an alert severity
// message from the FTP facility. The default severity is [SeverityEmerg];
// the default facility is [FacilityKern].
type Priority int

const severityMask = 0x07
const facilityMask = 0xf8

const (
	// Severity.

	// From /usr/include/sys/syslog.h.
	// These are the same on Linux, BSD, and OS X.
	SeverityEmerg Priority = iota
	SeverityAlert
	SeverityCrit
	SeverityErr
	SeverityWarning
	SeverityNotice
	SeverityInfo
	SeverityDebug
)

const (
	// Facility.

	// From /usr/include/sys/syslog.h.
	// These are the same up to LOG_FTP on Linux, BSD, and OS X.
	FacilityKern Priority = iota << 3
	FacilityUser
	FacilityMail
	FacilityDaemon
	FacilityAuth
	FacilitySyslog
	FacilityLpr
	FacilityNews
	FacilityUucp
	FacilityCron
	FacilityAuthPriv
	FacilityFtp
	_ // unused
	_ // unused
	_ // unused
	_ // unused
	FacilityLocal0
	FacilityLocal1
	FacilityLocal2
	FacilityLocal3
	FacilityLocal4
	FacilityLocal5
	FacilityLocal6
	FacilityLocal7
)

var (
	ErrInvalidNetwork  = fmt.Errorf("syslog: invalid network")
	ErrInvalidPriority = fmt.Errorf("syslog: invalid priority")
)

// A Writer is a connection to a syslog server.
type Writer struct {
	priority string
	hostname string
	procid   string
	appName  string

	network string
	address string
	needsLF bool

	mu   sync.Mutex // guards buf and conn
	buf  *bytes.Buffer
	conn net.Conn
}

// Dial establishes a connection to a log daemon by connecting to
// address addr on the specified network.
func Dial(network, address string, severity, facility Priority, appName string) (*Writer, error) {
	if !isValidNetwork(network) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidNetwork, network)
	}

	priority := (facility & facilityMask) | (severity & severityMask)
	if priority < 0 || priority > FacilityLocal7|SeverityDebug {
		return nil, fmt.Errorf("%w: %d", ErrInvalidPriority, priority)
	}

	hostname, err := os.Hostname()
	if err != nil && hostname == "" {
		hostname = "-"
	}

	w := &Writer{
		priority: strconv.FormatUint(uint64(priority), 10),
		hostname: hostname,
		procid:   strconv.FormatInt(int64(os.Getpid()), 10),
		appName:  appName,
		network:  network,
		address:  address,
		needsLF:  strings.HasPrefix(network, "tcp"),
		mu:       sync.Mutex{},
		buf:      &bytes.Buffer{},
		conn:     nil,
	}

	// No need for locking here, we are the only ones with access.
	err = w.connect()
	if err != nil {
		return nil, err
	}

	return w, nil
}

// connect makes a connection to the syslog server.
// It must be called with w.mu held.
func (w *Writer) connect() (err error) {
	if w.conn != nil {
		// ignore err from close, it makes sense to continue anyway
		_ = w.conn.Close()
		w.conn = nil
	}

	w.conn, err = net.Dial(w.network, w.address)
	if err != nil {
		return err
	}

	return nil
}

// Close closes a connection to the syslog daemon.
func (w *Writer) Close() (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return nil
	}

	err = w.conn.Close()
	w.conn = nil
	return err
}

func (w *Writer) Log(msg string) error {
	return w.write(msg)
}

// Write satisfies [io.Writer], however, it is not an [io.Writer] and lies about the number of
// bytes written to the syslog server.
func (w *Writer) Write(b []byte) (int, error) {
	return len(b), w.write(string(b))
}

// write generates and writes a syslog formatted string. The
// format is as follows: <PRI>1 TIMESTAMP HOSTNAME gorouter[-]: MSG
func (w *Writer) write(msg string) error {
	if w.conn == nil {
		err := w.connect()
		if err != nil {
			return err
		}
	}

	w.buf.Reset()

	w.buf.WriteRune('<')
	w.buf.WriteString(w.priority)
	w.buf.WriteString(">1 ")
	w.buf.WriteString(time.Now().Format(time.RFC3339))
	w.buf.WriteRune(' ')
	w.buf.WriteString(w.hostname)
	w.buf.WriteRune(' ')
	w.buf.WriteString(w.appName)
	w.buf.WriteRune(' ')
	w.buf.WriteString(w.procid)
	w.buf.WriteString(" - ")
	w.buf.WriteString(ByteOrderMark) // Unicode byte order mark, see RFC5424 section 6.4
	w.buf.WriteString(msg)

	// For TCP we use non-transparent framing as described in RFC6587 section 3.4.2.
	if w.needsLF {
		if !strings.HasSuffix(msg, "\n") {
			w.buf.WriteRune('\n')
		}
	}

	_, err := w.buf.WriteTo(w.conn)
	return err
}

var validNetworkPrefixes = []string{"tcp", "udp"}

func isValidNetwork(network string) bool {
	for _, p := range validNetworkPrefixes {
		if strings.HasPrefix(network, p) {
			return true
		}
	}

	return false
}
