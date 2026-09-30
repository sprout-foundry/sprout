package txn

import (
	"encoding/base64"
	"fmt"
	"os"
	"runtime"
	"time"
)

func encodeBase64(content []byte) string {
	return base64.StdEncoding.EncodeToString(content)
}

// formatFileMode renders a FileMode as the contract's octal string form
// ("0644"), matching how the browser side parses "mode".
//
// Windows has no permission bits: os.Stat synthesizes 0666 (0444 when
// read-only), which would land world-writable in a Linux container.
func formatFileMode(mode os.FileMode) string {
	perm := mode.Perm()
	if runtime.GOOS == "windows" {
		perm &^= 0o022
	}
	return fmt.Sprintf("%04o", uint32(perm))
}

// rollingBuffer keeps the LAST cap bytes written to it. A build can emit
// megabytes of output the platform has no use for; the contract keeps the
// tail, where the error usually is.
type rollingBuffer struct {
	buf []byte
	cap int
	// dropped counts bytes evicted from the front, so truncation is
	// detectable even when the retained tail happens to end on a boundary.
	dropped int64
}

func newRollingBuffer(cap int) *rollingBuffer {
	return &rollingBuffer{buf: make([]byte, 0, 1024), cap: cap}
}

func (r *rollingBuffer) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	if excess := len(r.buf) - r.cap; excess > 0 {
		r.buf = append(r.buf[:0], r.buf[excess:]...)
		r.dropped += int64(excess)
	}
	return len(p), nil
}

func (r *rollingBuffer) String() string { return string(r.buf) }

// truncated reports whether any byte was evicted.
func (r *rollingBuffer) truncated() bool { return r.dropped > 0 }

// normalizeTimeout applies the contract's default and hard cap. A zero or
// negative timeout means "use the default", never "run forever".
func normalizeTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = DefaultTimeoutSeconds
	}
	if seconds > MaxTimeoutSeconds {
		seconds = MaxTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}
