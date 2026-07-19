//go:build unix && !js

package console

import (
	"errors"

	"golang.org/x/sys/unix"
)

// maxPollEINTRRetries bounds how many times nonblockingEOFIsTransient
// will retry poll after unix.EINTR before giving up. Poll may return
// EINTR if a signal (notably SIGWINCH) lands in the EOF→poll window;
// retrying is the standard remedy.
const maxPollEINTRRetries = 3

// pollFn is the syscall entry point used by nonblockingEOFIsTransient.
// It is a package-level variable so tests can substitute deterministic
// fake implementations; production callers always see unix.Poll.
var pollFn = unix.Poll

// nonblockingEOFIsTransient reports whether a nonblocking read returning
// io.EOF should be treated as a transient "no data yet" condition rather
// than a permanent hangup.
//
// The discriminator is descriptor hangup state, not the EOF count: a
// pollable fd whose peer is still attached and merely idle can surface
// io.EOF on a nonblocking read, so only POLLHUP/POLLERR/POLLNVAL in
// poll's revents proves the fd is dead. When the fd cannot be polled
// (negative fd, or poll itself returns a non-EINTR error), the function
// fails safe and returns false so a real EOF is treated as fatal. EINTR
// is retried up to maxPollEINTRRetries; if every attempt is interrupted
// we return true because the next EOF iteration will probe again, and
// killing an interactive session on a transient signal would be a
// regression.
//
// Must only be called on a file descriptor currently in non-blocking
// mode; a blocking read that returns EOF is already authoritative and
// the caller should not be here in the first place.
func nonblockingEOFIsTransient(fd int) bool {
	if fd < 0 {
		return false
	}
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	// Zero timeout: inspect hangup state, don't wait for data.
	var n int
	var err error
	for attempt := 0; attempt < maxPollEINTRRetries; attempt++ {
		n, err = pollFn(pfd, 0)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EINTR) {
			// Non-EINTR error: cannot determine, fail safe.
			return false
		}
	}
	if err != nil {
		// All attempts interrupted: assume fd may still be alive
		// and let the next EOF iteration probe again.
		return true
	}
	if n <= 0 {
		// No revents: fd is alive and idle.
		return true
	}
	revents := pfd[0].Revents
	// POLLHUP — peer closed (pipe writer, controlling tty revoked,
	// SSH teardown); POLLERR — driver-level error; POLLNVAL — fd is
	// not open or not pollable. Any of these makes the fd unusable,
	// including POLLHUP set alongside POLLIN (writer closed; reads
	// will hit EOF on the next nonblocking attempt).
	if revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
		return false
	}
	// POLLIN/POLLPRI without hangup bits: fd is alive and readable.
	return true
}
