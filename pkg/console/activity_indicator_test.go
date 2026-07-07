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

// TestIndicator_StopOnIdleWritesNothing is the regression for the
// "word-by-word deleting" bug. When the indicator is fully idle (no
// spinner running, no static text), Stop() must NOT emit \r\033[K to the
// terminal. The streaming callback calls Stop() on every prose chunk; if
// each redundant Stop clobbered the current row, the streaming prose got
// erased character-by-character.
//
// We construct a TTY-mode indicator directly (bypassing NewActivityIndicator's
// fd-based TTY detection, which can't see a bytes.Buffer) so the TTY code
// path actually runs.
func TestIndicator_StopOnIdleWritesNothing(t *testing.T) {
	w := &nonTTYWriter{}
	a := &ActivityIndicator{
		w:     w,
		isTTY: true, // force the TTY code path
	}
	// Indicator is idle (never Start'd, never SetStatic'd). Stop() must
	// be a true no-op — no bytes written.
	a.Stop()
	if w.Len() != 0 {
		t.Errorf("Stop() on idle indicator should write nothing; got %d bytes (%q)", w.Len(), w.String())
	}

	// Calling Stop() again on the still-idle indicator must also write nothing.
	a.Stop()
	if w.Len() != 0 {
		t.Errorf("redundant Stop() on idle indicator should write nothing; got %d bytes (%q)", w.Len(), w.String())
	}
}

// TestIndicator_StopAfterClearStaticWritesNothing verifies that after
// SetStatic + ClearStatic (which leaves the indicator idle), subsequent
// Stop() calls also write nothing — the static text was already cleared by
// ClearStatic, so Stop has nothing to erase.
func TestIndicator_StopAfterClearStaticWritesNothing(t *testing.T) {
	w := &nonTTYWriter{}
	a := &ActivityIndicator{
		w:     w,
		isTTY: true,
	}
	a.SetStatic("pinned text")
	w.Reset() // discard the SetStatic render
	a.ClearStatic()
	// Now idle. ClearStatic wrote its own \r\033[K; verify it's there.
	if !strings.Contains(w.String(), "\033[K") {
		t.Fatalf("ClearStatic should have cleared the row; got %q", w.String())
	}
	w.Reset()
	// Subsequent Stop() must write nothing.
	a.Stop()
	if w.Len() != 0 {
		t.Errorf("Stop() after ClearStatic should write nothing; got %d bytes (%q)", w.Len(), w.String())
	}
}

// TestIndicator_StopErasesAfterStart verifies the fix didn't break the
// primary contract: after Start(), Stop() still erases the spinner row.
func TestIndicator_StopErasesAfterStart(t *testing.T) {
	w := &nonTTYWriter{}
	a := &ActivityIndicator{
		w:     w,
		isTTY: true,
	}
	a.Start("Thinking")
	// Give the render goroutine time to emit at least one frame.
	time.Sleep(3 * spinnerCadence)
	a.Stop()
	got := w.String()
	if !strings.Contains(got, "\r\033[K") {
		t.Errorf("Stop() after Start() should erase the spinner row (\\r\\033[K); got %q", got)
	}
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

// TestIndicator_RenderTruncatesLongMessage is the regression for the
// spinner line-wrap bug. When msg + elapsed suffix would exceed the terminal
// width, render() must truncate the message so the whole line fits on one row
// — otherwise the line wraps to a second physical row and \r on the next tick
// only clears the bottom row, leaving stale frames frozen above.
//
// We invoke render() directly (with a forced isTTY and a widthOverride) so we
// can assert against the exact output without racing the background goroutine.
func TestIndicator_RenderTruncatesLongMessage(t *testing.T) {
	w := &nonTTYWriter{}
	a := &ActivityIndicator{
		w:             w,
		isTTY:         true,
		widthOverride: 20, // narrow terminal
	}
	// Simulate an active spinner.
	a.active = true
	a.msg = "shell_command (go test ./... 2>&1 | tail -80) very long preview"
	a.startedAt = time.Now()

	a.render(0)
	out := w.String()

	// The rendered line must contain the ellipsis (truncation happened).
	if !strings.Contains(out, "…") {
		t.Errorf("long message should be truncated with …; got %q", out)
	}
	// And the visible width must not exceed the terminal width.
	visible := displayWidth(out)
	if visible > 20 {
		t.Errorf("rendered line visible width %d > terminal width 20; got %q", visible, out)
	}
}

// TestIndicator_RenderPreservesANSIBadge verifies that an ANSI-colored persona
// badge in the message survives truncation — the color escape bytes must be
// present in the rendered output.
func TestIndicator_RenderPreservesANSIBadge(t *testing.T) {
	// PersonaBadge's output depends on NO_COLOR/FORCE_COLOR env. Clear NO_COLOR
	// (which always wins per no-color.org) so the test asserts ANSI preservation
	// regardless of the caller's environment.
	t.Setenv("NO_COLOR", "")

	w := &nonTTYWriter{}
	a := &ActivityIndicator{
		w:             w,
		isTTY:         true,
		widthOverride: 16,
	}
	a.active = true
	badge := PersonaBadge(1, "coder") // "\033[36m[coder]\033[0m "
	a.msg = badge + "running a really long command that overflows"
	a.startedAt = time.Now()

	a.render(0)
	out := w.String()

	// The cyan color escape must still be present.
	if !strings.Contains(out, personaColorCoder) {
		t.Errorf("rendered line should preserve the persona cyan ANSI code; got %q", out)
	}
	// Visible width must respect the budget.
	if visible := displayWidth(out); visible > 16 {
		t.Errorf("rendered line visible width %d > 16; got %q", visible, out)
	}
}

// TestIndicator_RenderDoesNotTruncateShortMessage ensures the fix doesn't add
// a spurious ellipsis to messages that already fit.
func TestIndicator_RenderDoesNotTruncateShortMessage(t *testing.T) {
	w := &nonTTYWriter{}
	a := &ActivityIndicator{
		w:             w,
		isTTY:         true,
		widthOverride: 80,
	}
	a.active = true
	a.msg = "Thinking"
	a.startedAt = time.Now()

	a.render(0)
	out := w.String()
	if strings.Contains(out, "…") {
		t.Errorf("short message should not be truncated; got %q", out)
	}
	if !strings.Contains(out, "Thinking") {
		t.Errorf("short message text should be intact; got %q", out)
	}
}

// TestIndicator_SetStaticTruncatesLongLine mirrors the render() regression for
// SetStatic(): a long static line must be truncated to terminal width.
func TestIndicator_SetStaticTruncatesLongLine(t *testing.T) {
	w := &nonTTYWriter{}
	a := &ActivityIndicator{
		w:             w,
		isTTY:         true,
		widthOverride: 15,
	}
	a.SetStatic("this is a very long static line that should be truncated to fit")

	out := w.String()
	if !strings.Contains(out, "…") {
		t.Errorf("long static line should be truncated with …; got %q", out)
	}
	if visible := displayWidth(stripClearCodes(out)); visible > 15 {
		t.Errorf("static line visible width %d > 15; got %q", visible, out)
	}
}

// stripClearCodes removes the leading "\r\033[K" clear sequence so displayWidth
// measures only the visible content SetStatic wrote.
func stripClearCodes(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, "\r"), "\033[K")
}
