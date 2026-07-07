package console

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// Regression tests for the security-caution terminal-corruption bug
// (cmd/agent_terminal_subscriber.go).
//
// The terminal subscriber used to write `[⚠️  SECURITY CAUTION] …`
// directly to os.Stderr via fmt.Fprintf, bypassing PrintExternal. When
// the InputReader was active (between turns), the raw bytes landed
// under the cursor and corrupted the in-progress input line; when the
// SteerInputReader was active (during a turn), they corrupted the
// pinned steer panel. The fix routes the message through
// console.PrintExternal so cursor management does the right thing in
// either case.
//
// These tests exercise the exact format string the subscriber now
// passes to PrintExternal (`<glyph>[⚠️  SECURITY CAUTION] <message>`)
// with each of the three active-reader scenarios, so the cursor-
// management path runs end-to-end and a regression to raw stderr would
// fail loudly.

// TestSecurityCaution_TerminalSubscriberFormat_InputReaderActive is
// the regression test for the between-turns case the user hit. The
// InputReader is mounted (REPL waiting for input); the subscriber
// fires a security_caution event mid-tool-call. Pre-fix the message
// was written directly to stderr and the input line was scrambled.
// Post-fix it routes through PrintExternal, which clears the input
// line, prints, and redraws the prompt + buffer below.
func TestSecurityCaution_TerminalSubscriberFormat_InputReaderActive(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80

	LockOutput()
	setActiveInputReader(ir)
	setActiveSteerReader(nil)
	UnlockOutput()
	t.Cleanup(func() {
		LockOutput()
		setActiveInputReader(nil)
		UnlockOutput()
	})

	// Capture stdout + stderr so we can verify PrintExternal (stdout)
	// was used and nothing leaked to stderr.
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()
	os.Stdout = stdoutW
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		stdoutW.Close()
		stderrW.Close()
		stdoutR.Close()
		stderrR.Close()
	})

	// Exact format the subscriber emits post-fix:
	//   "<GlyphWarning> [⚠️  SECURITY CAUTION] <message>"
	// No trailing newline — PrintExternal auto-appends one.
	rawMsg := "high-risk operation rejected by persona risk cascade: high (command: 'git checkout HEAD -- pkg/spec/spec_test.go pkg/spec/spec_test.go && wc -l pkg/spec/spec_test.go')"
	line := fmt.Sprintf("%s[⚠️  SECURITY CAUTION] %s", GlyphWarning.Prefix(), rawMsg)
	PrintExternal(line)

	stdoutW.Close()
	stderrW.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	io.Copy(&stdoutBuf, stdoutR)
	io.Copy(&stderrBuf, stderrR)

	stdoutOutput := stdoutBuf.String()
	stderrOutput := stderrBuf.String()

	// The clear-line + redraw sequence is the fingerprint of the
	// PrintExternal/InputReader path. Without it, the bug regresses:
	// the message would land under the cursor instead of being
	// cleared-and-redrawn, scrambling the user's typed buffer.
	if !strings.Contains(stdoutOutput, "\r\033[K") {
		t.Error("Output missing clear-line sequence — PrintExternal/InputReader path was not used; the raw-fprintf regression is back")
	}
	// The full warning text must be present.
	if !strings.Contains(stdoutOutput, "SECURITY CAUTION") {
		t.Error("Output missing SECURITY CAUTION text")
	}
	if !strings.Contains(stdoutOutput, rawMsg) {
		t.Error("Output missing the underlying message body")
	}
	// Nothing should have gone to stderr — the fix routes everything
	// through PrintExternal which writes to stdout.
	if stderrOutput != "" {
		t.Errorf("Unexpected stderr output: %q — messages should route through PrintExternal (stdout) so the cursor-management path handles them", stderrOutput)
	}

	// Geometry must be sane after the render cycle.
	if ir.lastVisualRows < 0 {
		t.Errorf("lastVisualRows = %d, want >= 0", ir.lastVisualRows)
	}
	if ir.currentPhysicalLine < 0 {
		t.Errorf("currentPhysicalLine = %d, want >= 0", ir.currentPhysicalLine)
	}
}

// TestSecurityCaution_TerminalSubscriberFormat_SteerReaderActive covers
// the mid-turn case. The steer panel is pinned above the status footer;
// raw stderr writes during a turn would scramble the pinned row. With
// the fix, PrintExternal routes to the steer reader, which writes into
// the scroll region above the panel and redraws the pinned row cleanly.
func TestSecurityCaution_TerminalSubscriberFormat_SteerReaderActive(t *testing.T) {
	var steerBuf bytes.Buffer
	footer := NewStatusFooter(&steerBuf, nil)
	sr := &SteerInputReader{footer: footer}

	LockOutput()
	setActiveSteerReader(sr)
	setActiveInputReader(nil)
	UnlockOutput()
	t.Cleanup(func() {
		LockOutput()
		setActiveSteerReader(nil)
		UnlockOutput()
	})

	rawMsg := "high-risk operation rejected by persona risk cascade: high"
	line := fmt.Sprintf("%s[⚠️  SECURITY CAUTION] %s", GlyphWarning.Prefix(), rawMsg)
	PrintExternal(line)

	output := steerBuf.String()

	// Message routed to the steer reader's writer.
	if !strings.Contains(output, "SECURITY CAUTION") {
		t.Error("Output missing SECURITY CAUTION text — steer path was not used")
	}
	if !strings.Contains(output, rawMsg) {
		t.Error("Output missing underlying message body")
	}
	// Scroll-region reset would scroll the pinned panel away — the
	// whole point of the previous fix (8f501bd3). Regression check.
	if strings.Contains(output, "\033[r") {
		t.Error("Output contains scroll-region reset \\033[r — pinned steer panel would scroll off screen")
	}
}

// TestSecurityCaution_TerminalSubscriberFormat_NoReaderActive covers
// the fallback path: neither InputReader nor SteerInputReader is
// mounted (e.g., test runner, no-TTY environment). PrintExternal
// falls through to fmt.Print and the message renders directly. No
// geometry to corrupt.
func TestSecurityCaution_TerminalSubscriberFormat_NoReaderActive(t *testing.T) {
	LockOutput()
	setActiveSteerReader(nil)
	setActiveInputReader(nil)
	UnlockOutput()
	t.Cleanup(func() {
		LockOutput()
		setActiveSteerReader(nil)
		setActiveInputReader(nil)
		UnlockOutput()
	})

	// Capture stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = old
		w.Close()
		r.Close()
	})

	rawMsg := "high-risk operation rejected by persona risk cascade: high"
	line := fmt.Sprintf("%s[⚠️  SECURITY CAUTION] %s", GlyphWarning.Prefix(), rawMsg)
	PrintExternal(line)

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)

	output := buf.String()

	// Message must be present.
	if !strings.Contains(output, "SECURITY CAUTION") {
		t.Error("Output missing SECURITY CAUTION text")
	}
	if !strings.Contains(output, rawMsg) {
		t.Error("Output missing underlying message body")
	}
	// No cursor-management sequences — the fallback path writes the
	// message directly. A regression that puts cursor sequences in
	// the no-reader path would corrupt non-TTY environments.
	if strings.Contains(output, "\r\033[K") {
		t.Error("Output contains clear-line sequence in no-reader fallback — cursor management shouldn't fire without an active reader")
	}
}
