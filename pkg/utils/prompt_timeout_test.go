package utils

import (
	"bufio"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReadLineWithTimeout_ReturnsLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("yes\n"))
	line, err := ReadLineWithTimeout(r, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(line) != "yes" {
		t.Fatalf("got %q, want %q", line, "yes")
	}
}

func TestReadLineWithTimeout_TimesOutOnIdleStdin(t *testing.T) {
	// A reader that never produces a newline and never errors: blocks until
	// the deadline, mirroring an open-but-idle stdin (user walked away).
	r := bufio.NewReader(blockingReader{})
	start := time.Now()
	_, err := ReadLineWithTimeout(r, 20*time.Millisecond)
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
