package console

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// captureReadErrorStderr runs fn with os.Stderr redirected to a pipe and
// returns whatever was written. Used by the portable handleReadError
// tests to assert that no EOF diagnostic was emitted (transient EOF
// must be silent) or that the expected diagnostic was emitted (fatal
// EOF must log a message).
func captureReadErrorStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = original
		_ = w.Close()
		_ = r.Close()
	}()

	fn()
	os.Stderr = original
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(output)
}

// TestHandleReadError_BlockingEOFIsFatal verifies that when the caller
// passes nonBlocking=false, an io.EOF is always treated as fatal — the
// platform hangup helper is not consulted because the caller is in
// blocking mode and EOF there is authoritative.
//
// Portable: this path does not depend on Unix-only poll semantics.
func TestHandleReadError_BlockingEOFIsFatal(t *testing.T) {
	ir := NewInputReader("> ")
	var continueLoop bool
	var returnedErr error

	stderr := captureReadErrorStderr(t, func() {
		continueLoop, returnedErr = ir.handleReadError(io.EOF, false, nil, nil)
	})

	if continueLoop {
		t.Fatal("expected blocking EOF to stop")
	}
	if !errors.Is(returnedErr, io.EOF) {
		t.Fatalf("expected error to wrap io.EOF, got %v", returnedErr)
	}
	if !strings.Contains(stderr, "[console] stdin EOF") {
		t.Fatalf("expected EOF diagnostic, got %q", stderr)
	}
}

// TestHandleReadError_NonEOF_NoDiagnostic verifies that non-EOF errors
// do not trigger the EOF diagnostic. The handler always wraps the
// error in "stdin read error: ..." for context regardless of whether
// it is fatal or transient.
func TestHandleReadError_NonEOF_NoDiagnostic(t *testing.T) {
	ir := NewInputReader("> ")
	var continueLoop bool
	var returnedErr error

	stderr := captureReadErrorStderr(t, func() {
		continueLoop, returnedErr = ir.handleReadError(os.ErrPermission, false, nil, nil)
	})

	if continueLoop {
		t.Fatal("expected permission error to stop")
	}
	if returnedErr == nil {
		t.Fatal("expected non-nil error")
	}
	if stderr != "" {
		t.Fatalf("expected no diagnostic, got %q", stderr)
	}
}
