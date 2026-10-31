package console

import (
	"bytes"
	"strings"
	"testing"
)

// newTranscriptTestFooter builds a footer with a pinned geometry and no
// source, so tests exercise the region math without a pty.
func newTranscriptTestFooter(t *testing.T, cols, rows int) (*StatusFooter, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	f := NewStatusFooter(buf, nil)
	f.sizeOverride = &terminalSizeOverride{cols: cols, rows: rows}
	f.isTTY = true
	f.active = true
	RegisterGlobalStatusFooter(f)
	t.Cleanup(func() {
		RegisterGlobalStatusFooter(nil)
		f.sizeOverride = nil
	})
	return f, buf
}

func TestWriteTranscript_PassesTextThrough(t *testing.T) {
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("hello\n")
	if got := buf.String(); got != "hello\r\n" {
		t.Fatalf("got %q, want bare LF hardened to CRLF", got)
	}
}

func TestWriteTranscript_LeavesExistingCRLFAlone(t *testing.T) {
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("a\r\nb")
	if got := buf.String(); got != "a\r\nb" {
		t.Fatalf("got %q, want CRLF preserved and no trailing newline added", got)
	}
}

func TestWriteTranscript_ParksInsideRegionWhenFooterLive(t *testing.T) {
	f, footerBuf := newTranscriptTestFooter(t, 80, 24)
	_ = f
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("line\n")

	// The parking sequence must address the last row of the scroll region,
	// not the last row of the screen — rows below it belong to the footer.
	out := buf.String()
	if !strings.Contains(out, "\033[1;22r") {
		t.Fatalf("expected scroll region 1..22 for a 24-row screen with 2 reserved rows, got %q", out)
	}
	if !strings.Contains(out, "\033[22;1H") {
		t.Fatalf("expected cursor parked at row 22, got %q", out)
	}
	if !strings.HasSuffix(out, "line\r\n") {
		t.Fatalf("expected the text to follow the parking sequence, got %q", out)
	}
	_ = footerBuf
}

func TestWriteTranscript_RegionShrinksWithSteerPanel(t *testing.T) {
	f, _ := newTranscriptTestFooter(t, 80, 24)
	f.mu.Lock()
	f.steerActive = true
	f.steerLine = "> draft"
	f.steerWrappedActive = false
	f.mu.Unlock()

	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("line\n")

	// Steer panel adds a text row plus its rule, so the region bottom drops
	// from 22 to 20.
	if !strings.Contains(buf.String(), "\033[1;20r") {
		t.Fatalf("expected region to shrink to 1..20 with the steer panel up, got %q", buf.String())
	}
}

func TestWriteTranscript_NoFooterWritesAtCursor(t *testing.T) {
	RegisterGlobalStatusFooter(nil)
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("plain\n")
	if got := buf.String(); got != "plain\r\n" {
		t.Fatalf("expected no parking sequences without a footer, got %q", got)
	}
}

func TestBlankLine_SuppressesConsecutiveBlanks(t *testing.T) {
	RegisterGlobalStatusFooter(nil)
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("first\n")
	BlankLine()
	BlankLine()
	BlankLine()
	WriteTranscript("second\n")

	want := "first\r\nsecond\r\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q — consecutive blank lines must collapse", got, want)
	}
}

func TestBlankLine_EmitsWhenPreviousLineUnterminated(t *testing.T) {
	RegisterGlobalStatusFooter(nil)
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("no newline")
	BlankLine()

	want := "no newline\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBlankLine_SuppressedAtSessionStart(t *testing.T) {
	RegisterGlobalStatusFooter(nil)
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	BlankLine()
	if buf.Len() != 0 {
		t.Fatalf("expected no output at session start, got %q", buf.String())
	}
}

// TestTurnBoundary_RendersExactlyOneBlankRow pins the 171b contract: two
// consecutive turns must be separated by exactly one blank row, no matter
// how many blank-emitting call sites each turn passes through.
func TestTurnBoundary_RendersExactlyOneBlankRow(t *testing.T) {
	RegisterGlobalStatusFooter(nil)
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	// Turn 1: assistant prose ending on a fresh row, then the closing marker.
	WriteTranscript("assistant reply\n")
	WriteTranscript("⎯ turn over ⎯\n")

	// Turn 2: the header path. It used to be preceded by an unconditional
	// fmt.Println() in the REPL, which produced two blank rows here.
	SectionBreak()
	WriteTranscript("▌ assistant · model\n")

	got := buf.String()
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("two consecutive blank rows rendered:\n%q", got)
	}
	if !strings.Contains(got, "⎯ turn over ⎯\r\n\n▌ assistant") {
		t.Fatalf("expected exactly one blank row between turns, got:\n%q", got)
	}
}

// TestSectionBreak_CollapsesRuns pins the other half: two section breaks in
// a row (a turn that ends, then a path that also opens a section) must not
// stack into two blank rows.
func TestSectionBreak_CollapsesRuns(t *testing.T) {
	RegisterGlobalStatusFooter(nil)
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("turn one\n")
	SectionBreak()
	SectionBreak()
	SectionBreak()
	WriteTranscript("turn two\n")

	if got := buf.String(); got != "turn one\r\n\nturn two\r\n" {
		t.Fatalf("got %q, want exactly one blank row", got)
	}
}

func TestHardenNewlines(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"no newline", "no newline"},
		{"a\nb", "a\r\nb"},
		{"a\r\nb", "a\r\nb"},
		{"\n", "\r\n"},
		{"a\n\nb", "a\r\n\r\nb"},
	}
	for _, c := range cases {
		if got := hardenNewlines(c.in); got != c.want {
			t.Errorf("hardenNewlines(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPrintExternal_SteerActive_LandsInsideRegionAndClearsRow pins the
// "steer typed while lines are being written" case: mid-turn output must
// obey the same parking discipline as idle output, and must clear the row
// it lands on so a half-written line cannot survive underneath it.
func TestPrintExternal_SteerActive_LandsInsideRegionAndClearsRow(t *testing.T) {
	f, footerBuf := newTranscriptTestFooter(t, 80, 24)
	sr := &SteerInputReader{footer: f}

	LockOutput()
	setActiveSteerReader(sr)
	UnlockOutput()
	t.Cleanup(func() {
		LockOutput()
		setActiveSteerReader(nil)
		UnlockOutput()
	})

	PrintExternal("tool line")

	out := footerBuf.String()
	if !strings.Contains(out, "\033[1;22r") {
		t.Fatalf("mid-turn output did not re-assert the region: %q", out)
	}
	if !strings.Contains(out, "\033[22;1H\033[K") {
		t.Fatalf("mid-turn output parked without clearing the row: %q", out)
	}
	if !strings.Contains(out, "tool line\r\n") {
		t.Fatalf("message missing from output: %q", out)
	}
}

// TestWriteTranscript_ClearsParkedRow pins the same clear on the idle path.
func TestWriteTranscript_ClearsParkedRow(t *testing.T) {
	newTranscriptTestFooter(t, 80, 24)
	var buf bytes.Buffer
	restore := SetTranscriptWriterForTest(&buf)
	defer restore()

	WriteTranscript("line\n")
	if !strings.Contains(buf.String(), "\033[22;1H\033[K") {
		t.Fatalf("expected the parked row to be cleared before writing, got %q", buf.String())
	}
}
