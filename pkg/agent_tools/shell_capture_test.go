package tools

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBoundedCapture_UnderLimitIsVerbatim is the contract that matters most:
// a capture that never exceeds the limit must return exactly the bytes
// written, so ordinary command output is unaffected.
func TestBoundedCapture_UnderLimitIsVerbatim(t *testing.T) {
	var c BoundedCapture
	want := "line one\nline two\n"
	if _, err := c.Write([]byte(want)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := c.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if c.Truncated() {
		t.Fatal("Truncated() = true for output under the limit")
	}
	if got := c.Len(); got != int64(len(want)) {
		t.Fatalf("Len() = %d, want %d", got, len(want))
	}
}

// TestBoundedCapture_AtExactLimitIsVerbatim pins the boundary: exactly
// captureMaxBytes of output is retained in full, with no notice.
func TestBoundedCapture_AtExactLimitIsVerbatim(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), captureMaxBytes)

	var c BoundedCapture
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if c.Truncated() {
		t.Fatal("Truncated() = true at exactly the limit")
	}
	if got := c.String(); got != string(payload) {
		t.Fatalf("String() length = %d, want %d", len(got), len(payload))
	}
}

// TestBoundedCapture_OverLimitKeepsHeadAndTail verifies that oversized output
// retains the beginning and the end, drops the middle, and says so.
func TestBoundedCapture_OverLimitKeepsHeadAndTail(t *testing.T) {
	const total = 4 * captureMaxBytes

	var c BoundedCapture
	// Stream in chunks the way a pipe reader would, with a recognizable
	// marker at the very start and the very end.
	_, err := c.Write([]byte("HEAD-MARKER\n"))
	require.NoError(t, err)
	chunk := bytes.Repeat([]byte("x"), 4096)
	for written := len("HEAD-MARKER\n"); written < total; written += len(chunk) {
		_, err := c.Write(chunk)
		require.NoError(t, err)
	}
	_, err = c.Write([]byte("\nTAIL-MARKER\n"))
	require.NoError(t, err)

	got := c.String()

	if !c.Truncated() {
		t.Fatal("Truncated() = false for output over the limit")
	}
	if !strings.HasPrefix(got, "HEAD-MARKER\n") {
		t.Errorf("capture dropped the head: %q", got[:min(40, len(got))])
	}
	if !strings.HasSuffix(got, "\nTAIL-MARKER\n") {
		t.Errorf("capture dropped the tail: %q", got[max(0, len(got)-40):])
	}
	if !strings.Contains(got, "of output omitted") {
		t.Errorf("truncation notice missing from %q", got)
	}

	// The retained output must be bounded, notice included.
	if len(got) > captureMaxBytes+1024 {
		t.Errorf("retained %d bytes, want <= %d", len(got), captureMaxBytes+1024)
	}

	// Len reports the true stream length, not the retained length.
	if c.Len() < total {
		t.Errorf("Len() = %d, want >= %d", c.Len(), total)
	}
}

// TestBoundedCapture_LenCountsEveryByteAccepted guards io.MultiWriter
// semantics: Write must report the full length so a short write is never
// mistaken for an error by the copy loop.
func TestBoundedCapture_LenCountsEveryByteAccepted(t *testing.T) {
	var c BoundedCapture
	chunk := bytes.Repeat([]byte("y"), 8192)
	const writes = 100

	for i := 0; i < writes; i++ {
		n, err := c.Write(chunk)
		if err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		if n != len(chunk) {
			t.Fatalf("Write %d returned n=%d, want %d", i, n, len(chunk))
		}
	}

	want := int64(len(chunk) * writes)
	if got := c.Len(); got != want {
		t.Fatalf("Len() = %d, want %d", got, want)
	}
}

// TestBoundedCapture_ResetClearsState covers session reuse: a reset capture
// must not leak the previous command's output or its truncation flag.
func TestBoundedCapture_ResetClearsState(t *testing.T) {
	var c BoundedCapture
	if _, err := c.Write(bytes.Repeat([]byte("z"), 2*captureMaxBytes)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !c.Truncated() {
		t.Fatal("setup: expected truncated capture")
	}

	c.Reset()

	if c.Truncated() {
		t.Error("Truncated() = true after Reset")
	}
	if got := c.Len(); got != 0 {
		t.Errorf("Len() = %d after Reset, want 0", got)
	}
	if got := c.String(); got != "" {
		t.Errorf("String() = %q after Reset, want empty", got)
	}

	if _, err := c.Write([]byte("fresh")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := c.String(); got != "fresh" {
		t.Errorf("String() after Reset+Write = %q, want %q", got, "fresh")
	}
}

// TestBoundedCapture_ConcurrentWrites is the race guard for the stdout/stderr
// goroutines that share one capture.
func TestBoundedCapture_ConcurrentWrites(t *testing.T) {
	var c BoundedCapture
	var wg sync.WaitGroup
	const writers = 4
	const writes = 500

	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				// A BoundedCapture never fails; the error is asserted by
				// the single-writer tests above.
				_, _ = c.Write([]byte("concurrent output line\n"))
			}
		}()
	}

	// Read concurrently too — the PTY wait loop snapshots the capture while
	// the reader goroutine is still writing.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_ = c.String()
			_ = c.Len()
		}
	}()

	wg.Wait()
	<-done

	want := int64(writers * writes * len("concurrent output line\n"))
	if got := c.Len(); got != want {
		t.Fatalf("Len() = %d, want %d", got, want)
	}
}

// TestFormatByteCount covers the notice's unit rendering.
func TestFormatByteCount(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 bytes"},
		{512, "512 bytes"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	}
	for _, tc := range cases {
		if got := formatByteCount(tc.in); got != tc.want {
			t.Errorf("formatByteCount(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
