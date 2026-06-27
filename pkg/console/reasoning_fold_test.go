package console

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// stderrCapture redirects os.Stderr to a pipe.  Close() closes the write end
// (signalling EOF) and restores os.Stderr, after which the pipe's read end
// can be fully read into a buffer which it returns.
type stderrCapture struct {
	oldStderr *os.File
	r         *os.File
	w         *os.File
}

// captureStderr sets up an os.Pipe()-based stderr redirect.  Call Close()
// when the test is done writing, then read from the returned *os.File to
// get the captured output.
func captureStderr(t *testing.T) *stderrCapture {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stderr = w
	return &stderrCapture{
		oldStderr: old,
		r:         r,
		w:         w,
	}
}

// Close finalises the capture: closes the write end (EOF), restores
// os.Stderr, then copies everything that was written into a *bytes.Buffer
// which it returns.
func (c *stderrCapture) Close() *bytes.Buffer {
	c.w.Close()  // signal EOF to the reader
	os.Stderr = c.oldStderr
	buf := &bytes.Buffer{}
	_, _ = io.Copy(buf, c.r)
	c.r.Close()
	return buf
}

func TestReasoningFold_NilIndicator_DegradedMode(t *testing.T) {
	// With nil indicator, the fold operates in degraded (non-TTY) mode.
	cap := captureStderr(t)

	fold := NewReasoningFold(nil)

	// Start should print the initial header
	fold.Start()
	if !fold.IsActive() {
		t.Error("fold should be active after Start()")
	}

	// Chunks should accumulate tokens
	fold.Chunk("hello world")                       // 11 bytes / 4 = 2 tokens
	fold.Chunk("some more reasoning text here")     // 28 bytes / 4 = 7 tokens

	// Resolve should print summary
	fold.Resolve()
	if fold.IsActive() {
		t.Error("fold should not be active after Resolve()")
	}

	buf := cap.Close()
	if !strings.Contains(buf.String(), "⋯ thinking...") {
		t.Errorf("non-TTY Start should print initial header, got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "thought for") {
		t.Errorf("Resolve should print summary with token count, got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "tokens") {
		t.Errorf("Resolve summary should mention tokens, got: %q", buf.String())
	}
}

func TestReasoningFold_TokenEstimate_Accumulates(t *testing.T) {
	cap := captureStderr(t)

	fold := NewReasoningFold(nil)
	fold.Start()

	// "aaaa" = 4 bytes → 1 token
	fold.Chunk("aaaa")
	// "bbbbbbbb" = 8 bytes → 2 tokens
	fold.Chunk("bbbbbbbb")
	// "ccccccccccccc" = 13 bytes → 3 tokens
	fold.Chunk("ccccccccccccc")

	fold.Resolve()

	buf := cap.Close()
	// Total: 1 + 2 + 3 = 6 tokens
	if !strings.Contains(buf.String(), "6 tokens") {
		t.Errorf("expected '6 tokens' in output, got: %q", buf.String())
	}
}

func TestReasoningFold_IdempotentResolve(t *testing.T) {
	cap := captureStderr(t)

	fold := NewReasoningFold(nil)
	fold.Start()
	fold.Chunk("some reasoning")

	// We can't easily count Resolve calls, so verify by checking that
	// the output contains exactly one "thought for" line.
	fold.Resolve()
	fold.Resolve()
	fold.Resolve()

	buf := cap.Close()
	// Count how many "thought for" lines appear
	count := strings.Count(buf.String(), "thought for")
	if count != 1 {
		t.Errorf("expected exactly 1 'thought for' line after 3 Resolve() calls, got %d", count)
	}
}

func TestReasoningFold_IdempotentInterrupt(t *testing.T) {
	cap := captureStderr(t)

	fold := NewReasoningFold(nil)
	fold.Start()
	fold.Chunk("some reasoning")

	fold.Interrupt()
	fold.Interrupt()
	fold.Interrupt()

	buf := cap.Close()
	count := strings.Count(buf.String(), "thinking interrupted")
	if count != 1 {
		t.Errorf("expected exactly 1 'thinking interrupted' line after 3 Interrupt() calls, got %d", count)
	}
}

func TestReasoningFold_MultiBurst(t *testing.T) {
	cap := captureStderr(t)

	fold := NewReasoningFold(nil)

	// First burst
	fold.Start()
	fold.Chunk("burst one text")
	fold.Resolve()

	// Second burst
	fold.Start()
	fold.Chunk("burst two text here")
	fold.Resolve()

	buf := cap.Close()
	// Two bursts should produce two "thought for" lines
	count := strings.Count(buf.String(), "thought for")
	if count != 2 {
		t.Errorf("expected 2 'thought for' lines for two bursts, got %d in: %q", count, buf.String())
	}
}

func TestReasoningFold_Interrupt_PrintsDistinctSummary(t *testing.T) {
	cap := captureStderr(t)

	fold := NewReasoningFold(nil)
	fold.Start()
	fold.Chunk("some reasoning")
	fold.Interrupt()

	buf := cap.Close()
	if !strings.Contains(buf.String(), "thinking interrupted") {
		t.Errorf("expected 'thinking interrupted' in output, got: %q", buf.String())
	}
}

func TestReasoningFold_ActiveIndicator_NoPanic(t *testing.T) {
	// With a non-TTY indicator (e.g. a buffer), verify no panics occur
	buf := &bytes.Buffer{}
	// Create an indicator with a non-TTY writer (bytes.Buffer is never a TTY)
	indicator := NewActivityIndicator(buf)

	if indicator.IsTTY() {
		t.Skip("bytes.Buffer was detected as TTY (unexpected)")
	}

	fold := NewReasoningFold(indicator)
	fold.Start()
	fold.Chunk("reasoning text")
	fold.Resolve()

	// Should not panic and should be non-active
	if fold.IsActive() {
		t.Error("fold should not be active after Resolve()")
	}
}

func TestReasoningFold_ActiveIndicator_InterruptNoPanic(t *testing.T) {
	buf := &bytes.Buffer{}
	indicator := NewActivityIndicator(buf)

	if indicator.IsTTY() {
		t.Skip("bytes.Buffer was detected as TTY (unexpected)")
	}

	fold := NewReasoningFold(indicator)
	fold.Start()
	fold.Chunk("reasoning text")
	fold.Interrupt()

	if fold.IsActive() {
		t.Error("fold should not be active after Interrupt()")
	}
}
