package console

import (
	"io"
	"strconv"
	"strings"
	"testing"
)

// mirrorTestFooter installs a footer with a known conversation area so the
// mirror's capacity is the real thing rather than a fallback, sends transcript
// output to /dev/null, and returns the area's last row.
func mirrorTestFooter(t *testing.T, cols, rows int) (bottom int) {
	t.Helper()
	ResetTranscriptMirror()
	restore := SetTranscriptWriterForTest(io.Discard)
	t.Cleanup(restore)
	f := &StatusFooter{w: io.Discard, isTTY: true, fd: -1, steerCursor: -1}
	SetGeometryForTest(f, cols, rows)
	f.SetActiveForTest()
	prev := GetGlobalStatusFooter()
	RegisterGlobalStatusFooter(f)
	t.Cleanup(func() {
		RegisterGlobalStatusFooter(prev)
		ResetTranscriptMirror()
	})
	bottom = f.TranscriptBottomRow()
	if bottom <= 0 {
		t.Fatalf("fixture: no conversation area at %dx%d", cols, rows)
	}
	return bottom
}

// writeMirror sends s through the one transcript writer: these tests are about
// the mirror, not the escape stream.
func writeMirror(t *testing.T, s string) {
	t.Helper()
	WriteTranscript(s)
}

func TestTranscriptMirrorTrimsToTheConversationArea(t *testing.T) {
	const rows = 20
	bottom := mirrorTestFooter(t, 80, rows)

	var b strings.Builder
	for i := 1; i <= 30; i++ {
		b.WriteString("LINE-" + strconv.Itoa(i) + "\n")
	}
	writeMirror(t, b.String())

	lines, partial, _, ok := TranscriptRepaint()
	if !ok {
		t.Fatal("nothing to repaint after writing 30 lines")
	}
	if partial != "" {
		t.Fatalf("a newline-terminated write left a partial line %q", partial)
	}
	// The area's last row stays blank for the writer to park on, so one row
	// fewer than `bottom` carries content.
	if len(lines) != bottom-1 {
		t.Fatalf("mirror holds %d lines, want %d (area rows %d, last row parked on):\n%q",
			len(lines), bottom-1, bottom, lines)
	}
	if lines[len(lines)-1] != "LINE-30" {
		t.Fatalf("the newest line must be kept, last = %q", lines[len(lines)-1])
	}
	if lines[0] == "LINE-1" {
		t.Fatalf("the oldest lines should have scrolled out: %q", lines[0])
	}
}

// The mirror is trimmed to one geometry's capacity. Painted at a smaller one it
// would re-emit lines the terminal had already pushed into scrollback, so
// repainting is refused and the caller's narrower fallback runs.
func TestTranscriptRepaintRefusesAShrinkItCannotFit(t *testing.T) {
	_ = mirrorTestFooter(t, 80, 30)
	f := GetGlobalStatusFooter()

	var b strings.Builder
	for i := 0; i < 25; i++ {
		b.WriteString("L" + string(rune('A'+i)) + "\n")
	}
	writeMirror(t, b.String())
	before, _, contentRows, ok := TranscriptRepaint()
	if !ok || len(before) != 25 {
		t.Fatalf("fixture: ok=%v %d lines mirrored, want 25", ok, len(before))
	}
	if contentRows != 25 {
		t.Fatalf("content rows = %d, want 25", contentRows)
	}

	SetGeometryForTest(f, 80, 14)
	if _, _, _, ok := TranscriptRepaint(); ok {
		t.Fatal("repaint offered 25 mirrored lines to a 14-row terminal")
	}
}

func TestTranscriptRepaintRefusesUnknownRows(t *testing.T) {
	mirrorTestFooter(t, 80, 20)
	writeMirror(t, "MIRRORED-1\nMIRRORED-2\n")

	if _, _, _, ok := TranscriptRepaint(); !ok {
		t.Fatal("nothing to repaint for a mirrored transcript")
	}

	// A `!` command's output: on screen, but not something the console can
	// re-emit.
	markTranscriptUnmirrored()
	if _, _, _, ok := TranscriptRepaint(); ok {
		t.Fatal("repaint offered to clear a row it never saw")
	}

	// Once the transcript fills the area, the unknown row has scrolled out of
	// view and the repaint is safe again.
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("LATER-" + string(rune('A'+i/26)) + string(rune('A'+i%26)) + "\n")
	}
	writeMirror(t, b.String())
	lines, _, _, ok := TranscriptRepaint()
	if !ok {
		t.Fatal("repaint still refused after the unknown row scrolled out")
	}
	for _, l := range lines {
		if l == "" {
			t.Fatalf("the unknown row is still in the repaint set: %q", lines)
		}
	}
}

func TestTranscriptMirrorCountsWrappedLines(t *testing.T) {
	bottom := mirrorTestFooter(t, 20, 20)

	// Each line is 40 columns wide at a 20-column terminal: two rows apiece.
	long := strings.Repeat("x", 40) + "\n"
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString(long)
	}
	writeMirror(t, b.String())

	lines, _, _, ok := TranscriptRepaint()
	if !ok {
		t.Fatal("nothing to repaint")
	}
	if want := (bottom - 1) / 2; len(lines) != want {
		t.Fatalf("mirror holds %d wrapped lines, want %d (2 rows each in %d rows)",
			len(lines), want, bottom)
	}
}

func TestTranscriptMirrorTracksThePartialLine(t *testing.T) {
	mirrorTestFooter(t, 80, 20)
	writeMirror(t, "FIRST\nSECOND")

	if TranscriptAtLineStart() {
		t.Fatal("an unterminated write must leave the line-start flag clear")
	}
	lines, partial, _, ok := TranscriptRepaint()
	if !ok {
		t.Fatal("nothing to repaint")
	}
	if partial != "SECOND" {
		t.Fatalf("partial = %q, want the line in progress", partial)
	}
	if len(lines) != 1 || lines[0] != "FIRST" {
		t.Fatalf("lines = %q, want only the completed one", lines)
	}

	// The mid-line case is the one where the area's last row carries content,
	// so it holds one row more than a terminated write would.
	writeMirror(t, "\n")
	if !TranscriptAtLineStart() {
		t.Fatal("the newline must restore the line-start flag")
	}
	lines, partial, _, _ = TranscriptRepaint()
	if partial != "" || len(lines) != 2 {
		t.Fatalf("after the newline: %d lines, partial %q", len(lines), partial)
	}
}

func TestTranscriptMirrorBlankLinesTakeARow(t *testing.T) {
	bottom := mirrorTestFooter(t, 80, 8)
	writeMirror(t, "A\n\nB\n")

	lines, _, _, ok := TranscriptRepaint()
	if !ok {
		t.Fatal("nothing to repaint")
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %q, want A, the blank, and B", lines)
	}
	if lines[1] != "" {
		t.Fatalf("the blank row was dropped: %q", lines)
	}
	if bottom < 4 {
		t.Fatalf("fixture: area too small to check row counting (%d)", bottom)
	}
}
