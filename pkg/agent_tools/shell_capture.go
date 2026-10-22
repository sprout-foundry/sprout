package tools

// shell_capture.go — bounded in-memory capture of shell output.
//
// Every shell execution path in this package keeps a copy of the command's
// output in memory so it can be returned to the caller: the PTY wait loop
// (pkg/webui terminal_agent_exec.go), the os/exec adoptable path
// (shell_native.go), and the password-aware scanner
// (shell_native_password.go). Those copies must be bounded. A command like
// `journalctl -b -p err --no-pager` on a multi-GB journal, or a `cat` of a
// large file, produces output far beyond what a tool result may carry
// (truncateToolResult caps it at 50 KB), and nothing upstream limits it:
// the process keeps writing until it exits, however long that takes.
//
// Unbounded capture is not a transient cost. An output-heavy tool call that
// times out is promoted to a background session and its capture buffer
// stays reachable from the goroutine that is still reading the pipe, so the
// memory is not reclaimed until the command finally exits — potentially
// hours later. On a memory-constrained host that turns one careless
// command into an OOM.
//
// BoundedCapture keeps the head of the stream (where errors and context
// usually appear) and a rolling tail (where the final result usually
// appears), and counts what it dropped so the caller can say so. The head
// is taken from the start of the stream, so a capture that never exceeds
// the limit is byte-for-byte the output.

import (
	"bytes"
	"sync"
)

const (
	// captureHeadBytes is how much of the beginning of a stream is kept
	// verbatim. Matches the head window truncateToolResult shows
	// (45 KB), so a capture that is later truncated loses nothing the
	// user would otherwise have seen.
	captureHeadBytes = 45 * 1024

	// captureTailBytes is how much of the end of a stream is kept. Kept
	// in a fixed-size ring so a long-running command's memory footprint
	// does not grow with its output.
	captureTailBytes = 16 * 1024

	// captureMaxBytes is the total retained bytes per capture.
	captureMaxBytes = captureHeadBytes + captureTailBytes
)

// BoundedCapture accumulates a byte stream up to captureMaxBytes, keeping
// the head verbatim and a rolling tail of the most recent bytes. It is safe
// for concurrent use: the stdout and stderr readers of a single command may
// write to it from separate goroutines.
type BoundedCapture struct {
	mu sync.Mutex

	head    bytes.Buffer
	tail    []byte
	tailLen int // bytes currently held in tail
	tailPos int // next write position in tail (ring)

	// dropped counts bytes accepted but not retained. It is the
	// difference between what the command produced and what String
	// returns, and drives the truncation notice.
	dropped int64
}

// NewBoundedCapture returns an empty capture.
func NewBoundedCapture() *BoundedCapture {
	return &BoundedCapture{tail: make([]byte, captureTailBytes)}
}

// Write implements io.Writer. It never fails and never blocks on capacity:
// bytes beyond the retention limit are counted and discarded.
func (c *BoundedCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tail == nil {
		c.tail = make([]byte, captureTailBytes)
	}

	n := len(p)

	// Fill the head first.
	if room := captureHeadBytes - c.head.Len(); room > 0 && len(p) > 0 {
		take := min(room, len(p))
		c.head.Write(p[:take])
		p = p[take:]
	}

	// Everything after the head belongs to the rolling tail.
	for _, b := range p {
		if c.tailLen < captureTailBytes {
			c.tailLen++
		} else {
			// Ring is full: the byte at tailPos is the oldest retained
			// byte and is about to be overwritten.
			c.dropped++
		}
		c.tail[c.tailPos] = b
		c.tailPos = (c.tailPos + 1) % captureTailBytes
	}

	return n, nil
}

// Len reports how many bytes of output the command produced (retained plus
// dropped).
func (c *BoundedCapture) Len() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int64(c.head.Len()) + int64(c.tailLen) + c.dropped
}

// Truncated reports whether any output was dropped.
func (c *BoundedCapture) Truncated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropped > 0
}

// String returns the retained output: the head, then a truncation notice
// when anything was dropped, then the tail. A capture that stayed under the
// limit returns exactly the bytes written.
func (c *BoundedCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.dropped == 0 {
		var b bytes.Buffer
		b.Write(c.head.Bytes())
		b.Write(c.tailBytesLocked())
		return b.String()
	}

	var b bytes.Buffer
	b.Write(c.head.Bytes())
	b.WriteString("\n[... ")
	b.WriteString(formatByteCount(c.dropped))
	b.WriteString(" of output omitted: the command produced ")
	b.WriteString(formatByteCount(int64(c.head.Len()) + int64(c.tailLen) + c.dropped))
	b.WriteString(", of which the first ")
	b.WriteString(formatByteCount(int64(c.head.Len())))
	b.WriteString(" and last ")
	b.WriteString(formatByteCount(int64(c.tailLen)))
	b.WriteString(" bytes are shown. Re-run with a narrower filter (head, tail, grep, -n) to see the rest ...]\n")
	b.Write(c.tailBytesLocked())
	return b.String()
}

// Reset discards the captured output and starts over. Used when a session is
// reused for a new command and the previous command's output must not leak
// into the new result.
func (c *BoundedCapture) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.head.Reset()
	c.tailLen = 0
	c.tailPos = 0
	c.dropped = 0
}

// tailBytesLocked returns the retained tail in stream order.
func (c *BoundedCapture) tailBytesLocked() []byte {
	if c.tailLen < captureTailBytes {
		return c.tail[:c.tailLen]
	}
	out := make([]byte, captureTailBytes)
	n := copy(out, c.tail[c.tailPos:])
	copy(out[n:], c.tail[:c.tailPos])
	return out
}

// formatByteCount renders a byte count as "1234 bytes" / "12.3 KB" /
// "1.2 MB" for the truncation notice.
func formatByteCount(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return formatOneDecimal(float64(n)/gb) + " GB"
	case n >= mb:
		return formatOneDecimal(float64(n)/mb) + " MB"
	case n >= kb:
		return formatOneDecimal(float64(n)/kb) + " KB"
	default:
		return itoa(n) + " bytes"
	}
}

// formatOneDecimal renders v with one decimal place, without pulling fmt
// into the hot path of a writer.
func formatOneDecimal(v float64) string {
	whole := int64(v)
	frac := int64((v-float64(whole))*10 + 0.5)
	if frac > 9 {
		frac = 9
	}
	return itoa(whole) + "." + itoa(frac)
}

// itoa renders a non-negative int64 in base 10.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
