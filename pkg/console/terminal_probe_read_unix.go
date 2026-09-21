//go:build unix && !js

package console

import (
	"errors"
	"math"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// probeReplyTerminator is the final byte of a CSI reply ("… c" for
// DA2). The read loop returns as soon as it is observed.
const probeReplyTerminator = 'c'

// readTTYReply reads from fd until the terminator byte appears, the
// deadline expires, or the fd errors. Returns the accumulated bytes.
// Used for reply-style terminal queries (DA2 fingerprinting) where the
// fd is BORROWED — typically the caller's stdin — and must never be
// closed by this path. Raw unix.Read syscalls leave the fd's lifecycle
// entirely with the owner; wrapping it in an *os.File would install a
// GC finalizer that closes it (see probeBottomAnchored).
//
// The call is bounded by the deadline: the fd is switched to
// non-blocking mode for the duration (restored on exit) and Read is
// only attempted when poll reports POLLIN. Either guard alone leaves a
// parking path — poll can report the fd with no event bits at the
// timeout boundary, and a concurrent reader can steal the bytes poll
// reported — and a blocking Read on a borrowed stdin then parks the
// caller for the rest of the run.
func readTTYReply(fd int, timeout time.Duration) []byte {
	if fd < 0 || fd > math.MaxInt32 {
		return nil
	}
	if restore := makeProbeNonBlocking(fd); restore != nil {
		defer restore()
	}
	var buf []byte
	deadline := time.Now().Add(timeout)
	chunk := make([]byte, 32)
	for time.Now().Before(deadline) {
		pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		n, err := unix.Poll(pfd, int(remaining/time.Millisecond))
		if err != nil && !errors.Is(err, unix.EINTR) {
			return buf
		}
		if n == 0 {
			continue // no data yet before deadline
		}
		if n < 0 {
			// ppoll can return -1/errno=0 when the timeout expires
			// (a Linux kernel quirk); x/sys surfaces that as n<0
			// with a nil error and no event bits. Treat as "no
			// data" — falling through to a Read here would park.
			continue
		}
		if pfd[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return buf
		}
		if pfd[0].Revents&unix.POLLIN == 0 {
			continue
		}
		rn, rerr := unix.Read(fd, chunk)
		if rn > 0 {
			buf = append(buf, chunk[:rn]...)
			if idx := strings.IndexByte(string(buf), probeReplyTerminator); idx >= 0 {
				return buf[:idx+1]
			}
		}
		if rerr != nil {
			// EAGAIN means "no bytes right now", not "fd dead" —
			// the fd is non-blocking for the whole call, so park
			// briefly and keep polling until the deadline.
			if errors.Is(rerr, unix.EAGAIN) || errors.Is(rerr, unix.EWOULDBLOCK) || errors.Is(rerr, unix.EINTR) {
				time.Sleep(pastePollInterval)
				continue
			}
			return buf
		}
	}
	return buf
}

// makeProbeNonBlocking sets O_NONBLOCK on fd for the duration of a
// readTTYReply call and returns the restore function (or nil when the
// flag state could not be read, leaving the POLLIN-only guard in
// place). Setting the flag is what makes the reply Read incapable of
// blocking; the saved value is restored so the owner's mode is
// untouched, including the REPL's own non-blocking SIGWINCH windows.
func makeProbeNonBlocking(fd int) func() {
	old, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return nil
	}
	if old&unix.O_NONBLOCK == 0 {
		if _, setErr := unix.FcntlInt(uintptr(fd), unix.F_SETFL, old|unix.O_NONBLOCK); setErr != nil {
			return nil
		}
	}
	return func() {
		unix.FcntlInt(uintptr(fd), unix.F_SETFL, old) //nolint:errcheck,gosec // best-effort restore of the owner's flag state
	}
}
