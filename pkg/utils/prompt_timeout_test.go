package utils

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLineReader_ReturnsLine(t *testing.T) {
	r := NewLineReader(-1, strings.NewReader("yes\n"))
	line, err := r.ReadLine(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(line) != "yes" {
		t.Fatalf("got %q, want %q", line, "yes")
	}
}

func TestLineReader_TimesOutOnIdleStdin(t *testing.T) {
	// A reader that never produces a newline and never errors: blocks until
	// the deadline, mirroring an open-but-idle stdin (user walked away).
	r := NewLineReader(-1, blockingReader{})
	start := time.Now()
	_, err := r.ReadLine(context.Background(), 20*time.Millisecond)
	if !errors.Is(err, ErrPromptTimeout) {
		t.Fatalf("got err %v, want ErrPromptTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

// blockingReader blocks forever on Read, simulating a stalled stdin that is
// open but produces no bytes.
type blockingReader struct{}

func (blockingReader) Read(p []byte) (int, error) {
	select {} // block forever
}

// A pollable descriptor (a pipe) that goes quiet must time out via the
// poll path — the production path for os.Stdin — and leave nothing parked.
func TestLineReader_PollPathTimesOut(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Skipf("os.Pipe failed: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })

	lr := NewLineReader(int(r.Fd()), r)
	start := time.Now()
	_, err = lr.ReadLine(context.Background(), 50*time.Millisecond)
	if !errors.Is(err, ErrPromptTimeout) {
		t.Fatalf("got err %v, want ErrPromptTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}

	// After the timeout the reader must still work: a later line arrives.
	if _, err := w.WriteString("later\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := lr.ReadLine(context.Background(), 2*time.Second)
	if err != nil {
		t.Fatalf("read after timeout: %v", err)
	}
	if line != "later" {
		t.Fatalf("got %q, want %q", line, "later")
	}
}

// A partial line read before a timeout stays queued, not dropped: the next
// ReadLine completes it.
func TestLineReader_PartialLineSurvivesTimeout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Skipf("os.Pipe failed: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })

	lr := NewLineReader(int(r.Fd()), r)
	if _, err := w.WriteString("he"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Wait for the bytes to land, then let the read time out with the
	// partial line unread-but-buffered... it must NOT be consumed: the
	// poll says readable, the read takes it, no newline → carried over.
	time.Sleep(50 * time.Millisecond)
	if _, err := lr.ReadLine(context.Background(), 50*time.Millisecond); !errors.Is(err, ErrPromptTimeout) {
		t.Fatalf("got err %v, want ErrPromptTimeout", err)
	}
	if _, err := w.WriteString("llo\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := lr.ReadLine(context.Background(), 2*time.Second)
	if err != nil {
		t.Fatalf("read after timeout: %v", err)
	}
	if line != "hello" {
		t.Fatalf("got %q, want %q", line, "hello")
	}
}
