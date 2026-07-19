//go:build unix && !js

package console

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// makeNonblockingPipe returns an os.Pipe with the read end switched to
// O_NONBLOCK. The caller owns both ends and must close them. O_NONBLOCK
// is required because nonblockingEOFIsTransient is only meaningful for
// non-blocking reads.
func makeNonblockingPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if err := unix.SetNonblock(int(r.Fd()), true); err != nil {
		_ = r.Close()
		_ = w.Close()
		t.Fatalf("SetNonblock on pipe reader: %v", err)
	}
	return r, w
}

// TestNonblockingEOFIsTransient_HealthyFdReturnsTrue exercises the
// helper directly: a pipe whose writer is still open must report
// transient so subsequent EOFs are treated as "fd is idle" rather
// than fatal. A healthy idle pipe polls with no events; POLLHUP
// indicates the writer/peer has closed and is fatal even when POLLIN
// is also set.
func TestNonblockingEOFIsTransient_HealthyFdReturnsTrue(t *testing.T) {
	r, w := makeNonblockingPipe(t)
	defer r.Close()
	defer w.Close()

	if !nonblockingEOFIsTransient(int(r.Fd())) {
		t.Fatal("healthy pipe (writer open) should report transient=true")
	}
}

// TestNonblockingEOFIsTransient_HungupFdReturnsFalse is the regression
// for the user's scenario: a pipe whose writer is closed must report
// transient=false so the EOF triggers the "[console] stdin EOF"
// diagnostic and the REPL exits.
func TestNonblockingEOFIsTransient_HungupFdReturnsFalse(t *testing.T) {
	r, w := makeNonblockingPipe(t)
	defer r.Close()

	// Close the writer; poll on the reader will now surface POLLHUP.
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	if nonblockingEOFIsTransient(int(r.Fd())) {
		t.Fatal("hung-up pipe (writer closed) should report transient=false")
	}
}

// TestNonblockingEOFIsTransient_InvalidFdReturnsFalse exercises the
// fail-safe path: a negative fd must not panic and must report
// transient=false.
func TestNonblockingEOFIsTransient_InvalidFdReturnsFalse(t *testing.T) {
	if nonblockingEOFIsTransient(-1) {
		t.Fatal("negative fd should report transient=false (fail safe)")
	}
}

// TestHandleReadError_NonBlockingEOFOnHealthyPipeIsTransient is the
// end-to-end analogue of the helper-direct test: a healthy pipe is
// wired into ir.termFd, an io.EOF is injected, and the handler must
// continue silently with no diagnostic.
func TestHandleReadError_NonBlockingEOFOnHealthyPipeIsTransient(t *testing.T) {
	r, w := makeNonblockingPipe(t)
	defer r.Close()
	defer w.Close()

	ir := NewInputReader("> ")
	ir.termFd = int(r.Fd())

	var continueLoop bool
	var returnedErr error
	stderr := captureReadErrorStderr(t, func() {
		continueLoop, returnedErr = ir.handleReadError(io.EOF, true, nil, nil)
	})

	if !continueLoop {
		t.Fatal("expected EOF on healthy pipe to be transient")
	}
	if returnedErr != nil {
		t.Fatalf("expected nil error, got %v", returnedErr)
	}
	if stderr != "" {
		t.Fatalf("expected no diagnostic on transient EOF, got %q", stderr)
	}
}

// TestHandleReadError_NonBlockingEOFOnHungupPipeIsFatal is the
// end-to-end regression: a pipe whose writer has been closed surfaces
// POLLHUP, and the next EOF must be fatal (handler returns the wrapped
// error and logs the diagnostic).
func TestHandleReadError_NonBlockingEOFOnHungupPipeIsFatal(t *testing.T) {
	r, w := makeNonblockingPipe(t)
	defer r.Close()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	ir := NewInputReader("> ")
	ir.termFd = int(r.Fd())

	var continueLoop bool
	var returnedErr error
	stderr := captureReadErrorStderr(t, func() {
		continueLoop, returnedErr = ir.handleReadError(io.EOF, true, nil, nil)
	})

	if continueLoop {
		t.Fatal("expected EOF on hung-up pipe to be fatal")
	}
	if !errors.Is(returnedErr, io.EOF) {
		t.Fatalf("expected error to wrap io.EOF, got %v", returnedErr)
	}
	if !strings.Contains(stderr, "[console] stdin EOF") {
		t.Fatalf("expected EOF diagnostic, got %q", stderr)
	}
	if !strings.Contains(returnedErr.Error(), "stdin read error") {
		t.Fatalf("expected wrapped stdin read error, got %v", returnedErr)
	}
}

// TestHandleReadError_RepeatedIdleEOFsStayTransient is the regression
// for the user's away-from-keyboard scenario: many consecutive idle
// EOFs on a healthy pipe must all be treated as transient, because
// poll never reports POLLHUP.
func TestHandleReadError_RepeatedIdleEOFsStayTransient(t *testing.T) {
	r, w := makeNonblockingPipe(t)
	defer r.Close()
	defer w.Close()

	ir := NewInputReader("> ")
	ir.termFd = int(r.Fd())

	stderr := captureReadErrorStderr(t, func() {
		for i := 0; i < 10; i++ {
			continueLoop, err := ir.handleReadError(io.EOF, true, nil, nil)
			if !continueLoop {
				t.Fatalf("EOF #%d on healthy pipe should be transient", i+1)
			}
			if err != nil {
				t.Fatalf("EOF #%d returned err=%v, want nil", i+1, err)
			}
		}
	})

	if stderr != "" {
		t.Fatalf("expected no diagnostic across repeated idle EOFs, got %q", stderr)
	}
}

// stubPollFn replaces pollFn for the duration of one test and restores
// it via t.Cleanup. Not safe under t.Parallel() because pollFn is a
// package-level mutable.
func stubPollFn(t *testing.T, fn func([]unix.PollFd, int) (int, error)) {
	t.Helper()
	orig := pollFn
	pollFn = fn
	t.Cleanup(func() { pollFn = orig })
}

// TestNonblockingEOFIsTransient_EINTRThenSuccess verifies that a single
// EINTR followed by a clean idle poll reports transient=true: the SIGWINCH
// in the EOF→poll window must not terminate a healthy interactive REPL.
func TestNonblockingEOFIsTransient_EINTRThenSuccess(t *testing.T) {
	calls := 0
	stubPollFn(t, func(fds []unix.PollFd, timeout int) (int, error) {
		if len(fds) > 0 {
			fds[0].Revents = 0
		}
		calls++
		if calls == 1 {
			return 0, unix.EINTR
		}
		return 0, nil
	})

	if !nonblockingEOFIsTransient(7) {
		t.Fatal("EINTR-then-success should report transient=true")
	}
	if calls != 2 {
		t.Fatalf("expected 2 poll calls, got %d", calls)
	}
}

// TestNonblockingEOFIsTransient_PersistentEINTRReturnsTransient verifies
// that if every poll attempt is interrupted, the helper returns
// transient=true so the next EOF iteration probes again rather than
// killing a possibly healthy session.
func TestNonblockingEOFIsTransient_PersistentEINTRReturnsTransient(t *testing.T) {
	calls := 0
	stubPollFn(t, func(fds []unix.PollFd, timeout int) (int, error) {
		if len(fds) > 0 {
			fds[0].Revents = 0
		}
		calls++
		return 0, unix.EINTR
	})

	if !nonblockingEOFIsTransient(7) {
		t.Fatal("persistent EINTR should report transient=true")
	}
	if calls != maxPollEINTRRetries {
		t.Fatalf("expected %d poll calls, got %d", maxPollEINTRRetries, calls)
	}
}

// TestNonblockingEOFIsTransient_NonEINTRPollErrorReturnsFalse verifies
// the fail-safe path: a non-EINTR poll error (e.g. EFAULT) means we
// cannot determine hangup state, so a real EOF must still be treated
// as fatal.
func TestNonblockingEOFIsTransient_NonEINTRPollErrorReturnsFalse(t *testing.T) {
	calls := 0
	stubPollFn(t, func(fds []unix.PollFd, timeout int) (int, error) {
		if len(fds) > 0 {
			fds[0].Revents = 0
		}
		calls++
		return 0, unix.EFAULT
	})

	if nonblockingEOFIsTransient(7) {
		t.Fatal("non-EINTR poll error should report transient=false")
	}
	if calls != 1 {
		t.Fatalf("non-EINTR error must not retry, got %d calls", calls)
	}
}
