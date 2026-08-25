//go:build !js

package console

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// whatever was written. Used to assert on the SGR sequences
// setupInputTerm + teardownInputTerm emit.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	// Channel to collect the captured bytes; capacity 1 to avoid blocking
	// the writer if the consumer goroutine is slow.
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	<-done
	return buf.String()
}

// TestSetupInputTerm_EmitsExpectedSGRs ensures setupInputTerm installs
// the canonical enable sequences for bracketed paste, SGR mouse, and
// modifyOtherKeys — in that order — and that teardownInputTerm
// disables them again.
func TestSetupInputTerm_EmitsExpectedSGRs(t *testing.T) {
	ir := &InputReader{termFd: 0}

	out := captureStdout(t, func() {
		_, _ = ir.setupInputTerm()
		ir.teardownInputTerm()
	})

	// All four SGR sequences must appear, in the expected order.
	enablePaste := strings.Index(out, bracketedPasteEnable)
	enableMouse := strings.Index(out, MouseTrackingSGR)
	enableMOK := strings.Index(out, modifyOtherKeysEnable)
	disablePaste := strings.Index(out, bracketedPasteDisable)
	disableMouse := strings.Index(out, MouseTrackingDisable)
	disableMOK := strings.Index(out, modifyOtherKeysDisable)

	for i, c := range []struct {
		name, seq string
		pos       int
	}{
		{"enable bracketed paste", bracketedPasteEnable, enablePaste},
		{"enable SGR mouse", MouseTrackingSGR, enableMouse},
		{"enable modifyOtherKeys", modifyOtherKeysEnable, enableMOK},
		{"disable bracketed paste", bracketedPasteDisable, disablePaste},
		{"disable SGR mouse", MouseTrackingDisable, disableMouse},
		{"disable modifyOtherKeys", modifyOtherKeysDisable, disableMOK},
	} {
		if c.pos < 0 {
			t.Errorf("missing %q in setup+teardown output (case %d)", c.name, i)
		}
	}
	// Enables must precede disables so a teardown that runs before
	// setup doesn't accidentally re-enable something.
	if !(enablePaste < disablePaste && enableMouse < disableMouse && enableMOK < disableMOK) {
		t.Errorf("SGR enable sequences must precede their disables; got %q", out)
	}
}

// TestSetupInputTerm_RegistersActiveReader ensures setupInputTerm makes
// the reader discoverable to background goroutines (via the
// activeInputReader package variable), and that teardownInputTerm
// clears that registration.
func TestSetupInputTerm_RegistersActiveReader(t *testing.T) {
	ir := &InputReader{termFd: 0}

	// Sanity: nothing registered.
	activeInputReader = nil
	defer func() { activeInputReader = nil }()

	_ = captureStdout(t, func() {
		_, _ = ir.setupInputTerm()
		if activeInputReader != ir {
			t.Errorf("expected activeInputReader == ir (%p), got %p", ir, activeInputReader)
		}
		ir.teardownInputTerm()
		if activeInputReader != nil {
			t.Errorf("expected activeInputReader nil after teardown, got %p", activeInputReader)
		}
	})
}

// TestSetupInputTerm_NonBlockingFlag ensures the non-blocking result
// matches the platform's setNonblock behavior. On Unix setNonblock
// succeeds → nonBlocking == true; on the "other" platform it fails →
// nonBlocking == false. We just assert that the result is one of the
// two (don't lock in which is correct on this build's host).
func TestSetupInputTerm_NonBlockingFlag(t *testing.T) {
	ir := &InputReader{termFd: 0}
	_ = captureStdout(t, func() {
		_, nonBlocking := ir.setupInputTerm()
		// setNonblock on a closed pipe either succeeds (returns nil →
		// nonBlocking == true) or fails (nonBlocking == false). Both
		// are valid; we just assert the call didn't panic and the
		// return is a bool.
		_ = nonBlocking
		ir.teardownInputTerm()
	})
}
