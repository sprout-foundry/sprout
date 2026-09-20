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
func readTTYReply(fd int, timeout time.Duration) []byte {
	if fd < 0 || fd > math.MaxInt32 {
		return nil
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
		if pfd[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return buf
		}
		rn, rerr := unix.Read(fd, chunk)
		if rn > 0 {
			buf = append(buf, chunk[:rn]...)
			if idx := strings.IndexByte(string(buf), probeReplyTerminator); idx >= 0 {
				return buf[:idx+1]
			}
		}
		if rerr != nil {
			// The probe can run while the REPL has the fd in
			// non-blocking mode (SIGWINCH mid-ReadLine): EAGAIN
			// means "no bytes right now", not "fd dead" — park
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
