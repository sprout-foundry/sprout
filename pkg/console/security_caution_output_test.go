package console

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// TestPrintExternal_SingleCall_GeometryReset verifies that a single
// PrintExternal call resets the InputReader's geometry to a sane state
// (lastVisualRows >= 0, currentPhysicalLine >= 0, lastWrapPending = false).
func TestPrintExternal_SingleCall_GeometryReset(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80

	// Set as active input reader so PrintExternal routes through it.
	LockOutput()
	setActiveInputReader(ir)
	UnlockOutput()
	t.Cleanup(func() {
		LockOutput()
		setActiveInputReader(nil)
		UnlockOutput()
	})

	// Capture stdout to verify the clear-line sequence is emitted.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = old
		w.Close()
		r.Close()
	})

	msg := "\n[⚠️  SECURITY CAUTION - LLM VERIFICATION REQUIRED] test warning\n"
	PrintExternal(msg)

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// After printExternalLocked, geometry should be reset.
	if ir.lastVisualRows < 0 {
		t.Errorf("lastVisualRows = %d, want >= 0", ir.lastVisualRows)
	}
	if ir.currentPhysicalLine < 0 {
		t.Errorf("currentPhysicalLine = %d, want >= 0", ir.currentPhysicalLine)
	}
	if ir.lastWrapPending {
		t.Error("lastWrapPending = true, want false after reset")
	}

	// The output should contain the clear-line sequence (\r\033[K).
	if !strings.Contains(output, "\r\033[K") {
		t.Error("Output missing clear-line sequence \\r\\033[K — lock-protected path was not used")
	}
}

// TestPrintExternal_RapidSuccession_NoGeometryCorruption is the regression
// test for the CLI REPL binding bug. Three rapid PrintExternal calls (the
// old three-call pattern: blank / warning / blank) must NOT corrupt the
// InputReader's geometry state. Each call goes through the lock-protected
// path independently and resets geometry; the final state must be sane.
func TestPrintExternal_RapidSuccession_NoGeometryCorruption(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80

	// Set as active input reader.
	LockOutput()
	setActiveInputReader(ir)
	UnlockOutput()
	t.Cleanup(func() {
		LockOutput()
		setActiveInputReader(nil)
		UnlockOutput()
	})

	// Simulate the old three-call pattern: three rapid PrintExternal calls.
	// Each call independently acquires the lock, clears the line, prints,
	// resets geometry, and redraws — no state should leak between calls.
	msgs := []string{
		"\n",
		"\n[⚠️  SECURITY CAUTION - LLM VERIFICATION REQUIRED] test message\n",
		"\n",
	}

	for i, msg := range msgs {
		PrintExternal(msg)

		// After each call, geometry must be sane.
		if ir.lastVisualRows < 0 {
			t.Errorf("After call %d: lastVisualRows = %d, want >= 0", i+1, ir.lastVisualRows)
		}
		if ir.currentPhysicalLine < 0 {
			t.Errorf("After call %d: currentPhysicalLine = %d, want >= 0", i+1, ir.currentPhysicalLine)
		}
		// lastWrapPending can be true or false depending on geometry,
		// but it must not cause negative cursor lines.
	}

	// Final state: geometry must be consistent.
	if ir.lastVisualRows < 0 {
		t.Errorf("Final lastVisualRows = %d, want >= 0", ir.lastVisualRows)
	}
	if ir.currentPhysicalLine < 0 {
		t.Errorf("Final currentPhysicalLine = %d, want >= 0", ir.currentPhysicalLine)
	}
}

// TestPrintExternal_NoActiveInputReader falls back to direct fmt.Print
// when no InputReader is active.
func TestPrintExternal_NoActiveInputReader(t *testing.T) {
	// Ensure no active input reader.
	LockOutput()
	setActiveInputReader(nil)
	UnlockOutput()

	// Capture stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	msg := "hello from PrintExternal\n"
	PrintExternal(msg)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	if buf.String() != msg {
		t.Errorf("PrintExternal output = %q, want %q", buf.String(), msg)
	}
}

// TestPrintExternal_AutoNewline verifies that PrintExternal appends a
// trailing newline when the message doesn't end with one.
func TestPrintExternal_AutoNewline(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80

	LockOutput()
	setActiveInputReader(ir)
	UnlockOutput()
	t.Cleanup(func() {
		LockOutput()
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

	// Message without trailing newline.
	msg := "no trailing newline"
	PrintExternal(msg)

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// The output should contain the message followed by a newline
	// (PrintExternal auto-appends one). The full output also includes
	// the clear-line prefix and the prompt redraw from refreshLocked.
	expected := msg + "\n"
	if !strings.Contains(output, expected) {
		t.Errorf("Output should contain %q + newline, got %q", msg, output)
	}
}

// TestPrintExternal_MultiLineSecurityCaution verifies the exact pattern
// used by seed_tool_registry.go: a single PrintExternal call with a
// multi-line security caution message.
func TestPrintExternal_MultiLineSecurityCaution(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80

	LockOutput()
	setActiveInputReader(ir)
	UnlockOutput()
	t.Cleanup(func() {
		LockOutput()
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

	safeMsg := "command blocked by persona risk cascade"
	msg := fmt.Sprintf("\n[⚠️  SECURITY CAUTION - LLM VERIFICATION REQUIRED] %s\n", safeMsg)
	PrintExternal(msg)

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	r.Close()

	// Verify clear-line sequence is present (lock-protected path).
	if !strings.Contains(output, "\r\033[K") {
		t.Error("Output missing clear-line sequence — lock-protected path was not used")
	}

	// Verify the warning text is in the output.
	if !strings.Contains(output, "SECURITY CAUTION") {
		t.Error("Output missing SECURITY CAUTION text")
	}

	// Geometry must be sane.
	if ir.lastVisualRows < 0 {
		t.Errorf("lastVisualRows = %d, want >= 0", ir.lastVisualRows)
	}
	if ir.currentPhysicalLine < 0 {
		t.Errorf("currentPhysicalLine = %d, want >= 0", ir.currentPhysicalLine)
	}
}
