package utils

// line_reader.go — a cancellable single-line reader for interactive
// prompts that never leaves a goroutine parked on stdin.
//
// The previous ReadLineWithTimeout raced a blocking bufio.ReadString
// against a timer and left the read goroutine parked on stdin after a
// timeout — together with bufio's read-ahead it swallowed the next line
// the user typed, so the steer reader or REPL prompt that took over
// after the prompt saw nothing. LineReader instead poll-gates every
// read on the descriptor (unix) and reads synchronously: a timeout or
// cancellation returns with nothing parked and every byte the user
// typed still queued on the descriptor for the next reader.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

// LineReader reads newline-terminated lines from one reader. Create one
// per prompt (the retry loop for invalid input reuses it, so bytes
// already buffered are not lost between attempts).
type LineReader struct {
	r   io.Reader
	fd  int
	buf []byte

	// mu serializes ReadLine calls: overlapping reads on one LineReader
	// would interleave the shared buffer.
	mu sync.Mutex
}

// NewLineReader builds a LineReader over r. fd is the descriptor behind
// r, used to poll-gate reads; pass -1 when r is not a descriptor-backed
// reader (test pipes, buffers) — reads then block and only resolve at
// their own pace, with the deadline enforced by a watchdog goroutine
// that waits on a channel, never on the reader itself.
func NewLineReader(fd int, r io.Reader) *LineReader {
	return &LineReader{r: r, fd: fd}
}

// ReadLine reads one newline-terminated line, waiting at most d for it
// (d <= 0 waits indefinitely, bounded only by ctx). Returns
// ErrPromptTimeout when the deadline elapsed with no line; a cancelled
// ctx propagates ctx.Err(). Bytes typed but not yet read stay queued on
// the descriptor — nothing parked, nothing dropped. (A partial line the
// reader already consumed into its buffer without a newline is dropped
// when the prompt returns; an incomplete line is not a valid answer.)
func (lr *LineReader) ReadLine(ctx context.Context, d time.Duration) (string, error) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	if line, rest, ok := cutLine(lr.buf); ok {
		lr.buf = rest
		return line, nil
	}

	if lr.fd < 0 || !pollGatedReads {
		return lr.readLineUnpollable(ctx, d)
	}

	var deadline time.Time
	if d > 0 {
		deadline = time.Now().Add(d)
	}
	chunk := make([]byte, 4096)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		wait := time.Duration(0)
		if !deadline.IsZero() {
			wait = time.Until(deadline)
			if wait <= 0 {
				return "", ErrPromptTimeout
			}
		}
		if !waitForReadable(lr.fd, wait) {
			continue
		}
		n, err := lr.r.Read(chunk)
		if n > 0 {
			lr.buf = append(lr.buf, chunk[:n]...)
			if line, rest, ok := cutLine(lr.buf); ok {
				lr.buf = rest
				return line, nil
			}
		}
		if err != nil {
			if isEAGAINErr(err) {
				continue
			}
			if err == io.EOF && len(lr.buf) > 0 {
				// EOF with a partial line: hand back what arrived, like
				// bufio.ReadString did, so the caller can decide.
				line := string(lr.buf)
				lr.buf = nil
				return line, io.EOF
			}
			return "", err
		}
	}
}

// readLineUnpollable covers readers with no usable readiness poll: test
// readers (fd < 0) and platforms without a poll primitive. The read runs
// in a goroutine so the deadline and ctx cancellation still fire; the
// goroutine parks on the reader — never on shared terminal state — and
// only touches its own local buffer, so a timeout leaves no shared-state
// race behind. Do NOT reuse a LineReader that took this path after a
// timeout (any reader on non-Unix, fd<0 readers anywhere): the parked
// goroutine still owns the reader, and a second goroutine would race it
// for bytes. On Unix the poll path is used instead and nothing parks —
// the no-swallow guarantee is Unix-only; here a timeout may still let
// the parked goroutine consume one later line into its discarded buffer.
func (lr *LineReader) readLineUnpollable(ctx context.Context, d time.Duration) (string, error) {
	type result struct {
		line string
		rest []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var collected []byte
		chunk := make([]byte, 4096)
		for {
			n, err := lr.r.Read(chunk)
			if n > 0 {
				collected = append(collected, chunk[:n]...)
				if line, rest, ok := cutLine(collected); ok {
					ch <- result{line, rest, nil}
					return
				}
			}
			if err != nil {
				line := ""
				if err == io.EOF && len(collected) > 0 {
					line = string(collected)
				}
				ch <- result{line, nil, err}
				return
			}
		}
	}()
	var timer <-chan time.Time
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		timer = t.C
	}
	select {
	case res := <-ch:
		// The outer goroutine holds lr.mu; storing the unread remainder
		// here is race-free and keeps it for the next ReadLine.
		lr.buf = res.rest
		return res.line, res.err
	case <-timer:
		return "", ErrPromptTimeout
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// cutLine splits the first newline-terminated line off b.
func cutLine(b []byte) (line string, rest []byte, ok bool) {
	idx := bytes.IndexByte(b, '\n')
	if idx < 0 {
		return "", nil, false
	}
	line = strings.TrimRight(string(b[:idx]), "\r")
	// Trailing lines of a multi-line paste stay queued in the buffer so
	// the prompt's next ReadLine (or, after the prompt returns, the next
	// reader) sees exactly what was typed.
	return line, b[idx+1:], true
}

func isEAGAINErr(err error) bool {
	return errors.Is(err, errEAGAIN) || errors.Is(err, errEWOULDBLOCK)
}
