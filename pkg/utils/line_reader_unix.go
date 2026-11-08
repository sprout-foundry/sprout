//go:build unix && !js

package utils

import (
	"time"

	"golang.org/x/sys/unix"
)

var (
	errEAGAIN      = unix.EAGAIN
	errEWOULDBLOCK = unix.EWOULDBLOCK
)

// pollGatedReads reports whether waitForReadable really gates reads.
var pollGatedReads = true

// waitForReadable blocks until fd has data ready or the timeout elapses.
// Returns true when data is ready (fail-open on poll errors so a read is
// never dropped), false when the timeout elapsed with nothing to read.
func waitForReadable(fd int, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	fds := []unix.PollFd{{
		Fd:     int32(fd),
		Events: unix.POLLIN,
	}}
	for {
		n, err := unix.Poll(fds, int(timeout/time.Millisecond))
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return true
		}
		return n > 0
	}
}
