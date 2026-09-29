//go:build js || !unix

package console

import "time"

// canReadTTYReply is false here: a terminal reply to a query cannot be
// read back, so it would surface later as typed input.
const canReadTTYReply = false

// readTTYReply is the non-Unix fallback: no syscall-level read is
// available for a borrowed fd, so the probe sees no reply and Termux
// detection degrades to the conservative top-anchored default. Same
// contract as the Unix build: the fd is never closed here.
func readTTYReply(fd int, timeout time.Duration) []byte {
	return nil
}
