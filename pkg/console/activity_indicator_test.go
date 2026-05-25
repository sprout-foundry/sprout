package console

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// nonTTYWriter is a non-*os.File writer so NewActivityIndicator's TTY
// detection returns false. The indicator should be a no-op against it.
type nonTTYWriter struct{ bytes.Buffer }

func TestIndicator_NoOpOnNonTTY(t *testing.T) {
	w := &nonTTYWriter{}
	a := NewActivityIndicator(w)

	a.Start("Thinking")
	time.Sleep(2 * spinnerCadence)
	a.Update("Still thinking")
	time.Sleep(2 * spinnerCadence)
	a.Stop()

	if w.Len() != 0 {
		t.Errorf("non-TTY writer should receive zero bytes from spinner; got %d (%q)", w.Len(), w.String())
	}
	if a.IsActive() {
		t.Error("spinner should not be active on non-TTY")
	}
}

func TestIndicator_ReplaceWritesLineEvenOnNonTTY(t *testing.T) {
	w := &nonTTYWriter{}
	a := NewActivityIndicator(w)

	a.Start("Thinking")
	a.Replace("✓ done")

	got := w.String()
	if !strings.Contains(got, "✓ done") {
		t.Errorf("Replace should write the line even on non-TTY; got %q", got)
	}
}

func TestIndicator_ReplaceLast_NonTTY_PrintsLineWithoutEscapes(t *testing.T) {
	// On non-TTY, ReplaceLast degenerates to plain Fprintln so logs
	// still see each iteration. No ANSI escapes should appear.
	w := &nonTTYWriter{}
	a := NewActivityIndicator(w)
	a.ReplaceLast("✓ collapsed × 3")
	got := w.String()
	if got != "✓ collapsed × 3\n" {
		t.Errorf("expected plain line, got %q", got)
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("non-TTY output should not contain ANSI escapes; got %q", got)
	}
}

func TestIndicator_ReplaceLastN_NonTTYDegradesToFprintln(t *testing.T) {
	w := &nonTTYWriter{}
	a := NewActivityIndicator(w)
	a.ReplaceLastN("collapsed line", 5)
	got := w.String()
	if got != "collapsed line\n" {
		t.Errorf("expected single Fprintln on non-TTY; got %q", got)
	}
}

func TestIndicator_ReplaceLastN_ClampsBelowOne(t *testing.T) {
	// n<1 should be treated as 1 (or no-op-equivalent) — we just
	// verify it doesn't panic and produces deterministic output. On a
	// non-TTY this collapses to Fprintln anyway, so the assertion is
	// just "no panic + line printed".
	w := &nonTTYWriter{}
	a := NewActivityIndicator(w)
	a.ReplaceLastN("line", 0)
	a.ReplaceLastN("line", -3)
	if !strings.Contains(w.String(), "line") {
		t.Errorf("expected line output despite n<1; got %q", w.String())
	}
}

func TestIndicator_NilSafe(t *testing.T) {
	var a *ActivityIndicator
	// None of these should panic.
	a.Start("x")
	a.Update("y")
	a.Stop()
	a.Replace("z")
	if a.IsActive() {
		t.Error("nil indicator should not report active")
	}
	if a.Elapsed() != 0 {
		t.Error("nil indicator should report zero elapsed")
	}
}

func TestSanitizeLine_StripsNewlinesAndCR(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"line1\nline2", "line1line2"},
		{"with\rCR", "withCR"},
		{"mixed\n\rstuff", "mixedstuff"},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizeLine(c.in); got != c.want {
			t.Errorf("sanitizeLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIndicator_UpdateNoOpWhenInactive(t *testing.T) {
	a := NewActivityIndicator(&nonTTYWriter{})
	// Update before Start — should not panic, should not activate.
	a.Update("ghost")
	if a.IsActive() {
		t.Error("Update should not activate a stopped spinner")
	}
}

func TestIndicator_StopIdempotent(t *testing.T) {
	a := NewActivityIndicator(&nonTTYWriter{})
	a.Stop()
	a.Stop()
	a.Stop()
	// No panic = pass.
}

// SP-048-2a: tab-completion cycle in InputReader.

func TestInputReader_TabCompletion_CycleAndReset(t *testing.T) {
	ir := NewInputReader("> ")

	calls := 0
	ir.SetCompleter(func(line string, cursorPos int) []string {
		calls++
		// Three candidates for prefix "/he"
		if line == "/he" {
			return []string{"/help", "/heart", "/heat"}
		}
		return nil
	})

	// Set the buffer to "/he"; cursor at end.
	ir.line = "/he"
	ir.cursorPos = len(ir.line)

	// First Tab: starts the cycle, picks first candidate.
	ir.handleTabCompletion()
	if ir.line != "/help" {
		t.Errorf("after 1st Tab, line = %q, want /help", ir.line)
	}
	if ir.completionCycle == nil || ir.completionCycle.index != 0 {
		t.Errorf("cycle index should be 0 after 1st Tab, got %+v", ir.completionCycle)
	}

	// Second Tab without typing: advances to next candidate.
	ir.handleTabCompletion()
	if ir.line != "/heart" {
		t.Errorf("after 2nd Tab, line = %q, want /heart", ir.line)
	}
	if ir.completionCycle == nil || ir.completionCycle.index != 1 {
		t.Errorf("cycle index should be 1 after 2nd Tab, got %+v", ir.completionCycle)
	}

	// Third Tab: third candidate.
	ir.handleTabCompletion()
	if ir.line != "/heat" {
		t.Errorf("after 3rd Tab, line = %q, want /heat", ir.line)
	}

	// Fourth Tab: wraps back to first.
	ir.handleTabCompletion()
	if ir.line != "/help" {
		t.Errorf("after 4th Tab (wrap), line = %q, want /help", ir.line)
	}

	// Completer should have been called only once — subsequent Tabs use
	// the cached cycle.
	if calls != 1 {
		t.Errorf("completer should be called exactly once during cycle, got %d", calls)
	}

	// Now simulate the user typing a character — buffer becomes "/helpx".
	ir.line = "/helpx"
	ir.cursorPos = len(ir.line)

	// Next Tab should re-query the completer with the new buffer.
	ir.SetCompleter(func(line string, cursorPos int) []string {
		calls++
		return nil // no matches
	})
	ir.handleTabCompletion()
	if ir.line != "/helpx" {
		t.Errorf("no matches should leave line unchanged, got %q", ir.line)
	}
}

func TestInputReader_TabCompletion_NoCompleter(t *testing.T) {
	ir := NewInputReader("> ")
	ir.line = "/help"
	ir.cursorPos = len(ir.line)
	// Without a completer set, Tab is a no-op and shouldn't panic.
	ir.handleTabCompletion()
	if ir.line != "/help" {
		t.Errorf("Tab without completer should leave line unchanged, got %q", ir.line)
	}
}
