package console

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestHandleReadError_EOF_Diagnostic(t *testing.T) {
	// Capture stderr to verify the diagnostic message is emitted.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = origStderr
		w.Close()
		r.Close()
	})

	// Create an InputReader with a non-terminal fd so term.IsTerminal returns false.
	// This exercises the "TTY detached" diagnostic path.
	ir := NewInputReader("> ")
	ir.termFd = int(r.Fd()) // pipe fd is not a terminal

	// Call handleReadError with io.EOF.
	continueLoop, returnedErr := ir.handleReadError(io.EOF, false, nil, nil)

	// Verify the error is wrapped correctly (existing behavior preserved).
	if continueLoop {
		t.Fatal("expected continueLoop=false for io.EOF")
	}
	if returnedErr == nil {
		t.Fatal("expected non-nil error for io.EOF")
	}
	if !strings.Contains(returnedErr.Error(), "stdin read error") {
		t.Fatalf("unexpected error format: %s", returnedErr)
	}
	if !errors.Is(returnedErr, io.EOF) {
		t.Fatalf("expected error to wrap io.EOF, got: %v", returnedErr)
	}

	// Flush stderr and read the captured output.
	w.Close()
	var buf strings.Builder
	_, err = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("copy stderr: %v", err)
	}
	stderrOutput := buf.String()

	// Verify the "TTY detached" diagnostic was emitted.
	if !strings.Contains(stderrOutput, "[console] stdin EOF: terminal no longer attached") {
		t.Fatalf("expected TTY-detached diagnostic in stderr, got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "fd=") {
		t.Fatalf("expected fd= in diagnostic, got: %q", stderrOutput)
	}
}

func TestHandleReadError_NonEOF_NoDiagnostic(t *testing.T) {
	// Verify that non-EOF errors do NOT trigger the diagnostic log.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = origStderr
		w.Close()
		r.Close()
	})

	ir := NewInputReader("> ")
	ir.termFd = int(r.Fd())

	// Use a non-EOF error.
	testErr := os.ErrPermission
	continueLoop, returnedErr := ir.handleReadError(testErr, false, nil, nil)

	if continueLoop {
		t.Fatal("expected continueLoop=false for permission error")
	}
	if returnedErr == nil {
		t.Fatal("expected non-nil error")
	}

	// Flush and check stderr is empty (no diagnostic for non-EOF).
	w.Close()
	var buf strings.Builder
	_, err = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("copy stderr: %v", err)
	}
	if buf.String() != "" {
		t.Fatalf("expected no stderr output for non-EOF error, got: %q", buf.String())
	}
}
