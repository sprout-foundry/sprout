package console

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/testutil/vtscreen"
)

// Resize assertions against a rendered screen rather than the escape stream.
// The older resize tests check which sequences Resize emitted; these check what
// the user can still read afterwards, which is the thing that regressed.

// resizeFixture is a footer pinned over an emulated terminal with a transcript
// already written into the conversation area.
type resizeFixture struct {
	f   *StatusFooter
	scr *vtscreen.Screen
}

func newResizeFixture(t *testing.T, cols, rows int, lines []string) *resizeFixture {
	t.Helper()
	ResetTranscriptMirror()
	t.Cleanup(ResetTranscriptMirror)
	scr := vtscreen.New(cols, rows)
	f := &StatusFooter{
		w:           scr,
		isTTY:       true,
		fd:          -1,
		steerCursor: -1,
		source:      &stubSource{model: "stub", used: 1000, limit: 200000, cost: 0.25, workdir: "/work"},
	}
	SetGeometryForTest(f, cols, rows)
	f.SetActiveForTest()
	RegisterGlobalStatusFooter(f)
	t.Cleanup(func() { RegisterGlobalStatusFooter(nil) })

	LockOutput()
	writeTranscriptLocked(scr, strings.Join(lines, "\n")+"\n")
	UnlockOutput()
	LockOutput()
	f.drawLocked()
	UnlockOutput()
	return &resizeFixture{f: f, scr: scr}
}

// resize moves the terminal and the footer's view of it, then runs the handler
// under test.
func (r *resizeFixture) resize(t *testing.T, cols, rows int) {
	t.Helper()
	r.scr.Resize(cols, rows)
	SetGeometryForTest(r.f, cols, rows)
	r.f.Resize()
}

// contentRows is the rows that carry conversation text: the conversation area,
// blanks dropped.
func (r *resizeFixture) contentRows() []string {
	_, rows := r.f.terminalSize()
	bottom := r.f.TranscriptBottomRow()
	visible := r.scr.Lines()
	if bottom > len(visible) {
		bottom = len(visible)
	}
	var out []string
	for _, line := range visible[:min(bottom, rows)] {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// footerPinned reports whether the footer's rule and content row own the
// screen's last two rows.
func (r *resizeFixture) footerPinned() bool {
	_, rows := r.scr.Size()
	return strings.Contains(r.scr.Line(rows-1), "─") &&
		strings.Contains(r.scr.Line(rows), "stub")
}

// assertKept fails when a row that was readable before the resize is gone, or
// shows up twice. A row that merely scrolled out of view counts as kept: the
// terminal's scrollback still holds it, which AllText models.
func assertKept(t *testing.T, want []string, after *vtscreen.Screen) {
	t.Helper()
	text := after.AllText()
	lines := strings.Split(text, "\n")
	for _, row := range want {
		n := 0
		for _, l := range lines {
			if l == row {
				n++
			}
		}
		switch {
		case n == 0:
			t.Errorf("row %q was erased by the resize\nscreen+scrollback:\n%s", row, text)
		case n > 1:
			t.Errorf("row %q appears %d times after the resize:\n%s", row, n, text)
		}
	}
}

// withResizeFlavor pins the terminal family Resize branches on.
func withResizeFlavor(t *testing.T, bottomAnchored bool) {
	t.Helper()
	old := probeOverride
	probeOverride = func() bool { return bottomAnchored }
	t.Cleanup(func() { probeOverride = old; resetFlavorCache() })
}

func transcriptLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "TRANSCRIPT-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
	}
	return out
}

// The reported bug: growing the terminal vertically deleted transcript rows.
func TestResizeGrowKeepsEveryVisibleRow(t *testing.T) {
	withResizeFlavor(t, false)
	r := newResizeFixture(t, 80, 20, transcriptLines(12))
	before := r.contentRows()
	if len(before) < 8 {
		t.Fatalf("fixture: expected a populated conversation area, got %q", before)
	}

	r.resize(t, 80, 26)

	assertKept(t, before, r.scr)
	if !r.footerPinned() {
		t.Errorf("the footer is not pinned to the new bottom:\n%s", r.scr.Dump())
	}
}

// A grow that also narrows: the footer's padded rows rewrap, and both the clear
// window and the gap-close shift by the wrap overflow.
func TestResizeGrowNarrowerKeepsEveryVisibleRow(t *testing.T) {
	withResizeFlavor(t, false)
	r := newResizeFixture(t, 80, 20, transcriptLines(12))
	before := r.contentRows()

	r.resize(t, 60, 26)

	assertKept(t, before, r.scr)
}

// A grow that also widens: the old footer's padded rows stop wrapping, so the
// stale block is shorter than the reserved count recorded at draw time.
func TestResizeGrowWiderKeepsEveryVisibleRow(t *testing.T) {
	withResizeFlavor(t, false)
	r := newResizeFixture(t, 80, 20, transcriptLines(12))
	before := r.contentRows()

	r.resize(t, 100, 24)

	assertKept(t, before, r.scr)
}

func TestResizeShrinkKeepsEveryVisibleRow(t *testing.T) {
	withResizeFlavor(t, false)
	r := newResizeFixture(t, 80, 20, transcriptLines(12))
	before := r.contentRows()

	r.resize(t, 80, 14)

	assertKept(t, before, r.scr)
}

// Termux anchors content to the bottom on a grow, so the rows above the old
// footer block hold freshly pulled-down conversation, not stale chrome.
func TestResizeBottomAnchoredGrowKeepsEveryVisibleRow(t *testing.T) {
	withResizeFlavor(t, true)
	r := newResizeFixture(t, 80, 20, transcriptLines(12))
	r.scr.SetResizeMode(vtscreen.ResizeBottomAnchored)
	before := r.contentRows()

	r.resize(t, 80, 26)

	assertKept(t, before, r.scr)
}
