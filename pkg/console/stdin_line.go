package console

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"time"
)

// pasteBurstWindow is how long ReadLineCtx keeps collecting after the first
// line arrives. A multi-line paste reaches the terminal in one burst, so its
// later lines are already queued by then; a person typing a second line
// takes far longer. Without this, the first line of a paste submits and the
// rest is left on stdin for whatever reads next — the steer box, or the REPL
// prompt, which would send it as a new message.
const pasteBurstWindow = 40 * time.Millisecond

// linePollInterval bounds how long each wait for input runs before ctx is
// checked again.
const linePollInterval = 50 * time.Millisecond

// ReadLineCtx reads one answer line from stdin in cooked mode: Enter submits.
// Lines that arrive in the same burst as the submit (a multi-line paste) are
// part of the answer and come back joined with "\n". The trailing newline is
// stripped.
//
// The read is cancellable without leaking: it waits for stdin to become
// readable before every Read and checks ctx between waits, so a cancelled
// prompt leaves no goroutine parked in Read to swallow the next keystrokes
// meant for the steer box or the REPL. Returns io.EOF when stdin closes
// before any input.
//
// On platforms without readiness polling (see waitForStdinReadable) the
// Read blocks until the user presses Enter, and ctx is observed after it.
func ReadLineCtx(ctx context.Context) (string, error) {
	return readLineCtx(ctx, int(os.Stdin.Fd()), os.Stdin) //nolint:gosec // G115: fd fits in int
}

func readLineCtx(ctx context.Context, fd int, r io.Reader) (string, error) {
	var buf bytes.Buffer
	chunk := make([]byte, 4096)
	readOnce := func() (bool, error) {
		n, err := r.Read(chunk)
		buf.Write(chunk[:n])
		return n > 0, err
	}
	// Wait for the first complete line.
	for !bytes.Contains(buf.Bytes(), []byte{'\n'}) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !waitForStdinReadable(fd, linePollInterval) {
			continue
		}
		if _, err := readOnce(); err != nil {
			if err == io.EOF && buf.Len() > 0 {
				break
			}
			return "", err
		}
	}
	// Collect the rest of a paste burst.
	for waitForStdinReadable(fd, pasteBurstWindow) {
		if got, err := readOnce(); err != nil || !got {
			break
		}
	}
	return strings.TrimRight(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n"), nil
}
