//go:build unix && !js

package console

import (
	"time"

	"golang.org/x/sys/unix"
)

// waitForStdinReadable blocks until fd has data ready or the timeout
// expires, whichever comes first. Returns true when data is ready (or
// the state can't be determined — fail-open so input is never lost),
// false when the timeout elapsed with nothing to read.
//
// This gates each stdin Read in the ReadLine loop so the loop can
// observe side-channel state (the wakeup flag set by InputReader.Wake)
// while the user is idle. Without it, os.Stdin.Read parks in Go's
// runtime netpoller and absorbs O_NONBLOCK — the flag would sit unseen
// until the next keystroke, defeating the whole wake mechanism.
func waitForStdinReadable(fd int, timeout time.Duration) bool {
	if fd < 0 {
		return true
	}
	fds := []unix.PollFd{{
		Fd:     int32(fd),
		Events: unix.POLLIN,
	}}
	for {
		n, err := unix.Poll(fds, int(timeout/time.Millisecond))
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			// Poll failed for an unexpected reason — fail open so the
			// caller's Read still runs (input is never dropped).
			return true
		}
		return n > 0
	}
}
