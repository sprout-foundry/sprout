package vtscreen

import (
	"strings"
	"testing"
)

// fill writes n numbered rows with a newline BETWEEN them, so the bottom row
// carries content rather than the blank a trailing newline would leave.
func fill(t *testing.T, s *Screen, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		if i > 1 {
			if _, err := s.WriteString("\n"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.WriteString("L" + string(rune('0'+i))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResizeNonReflowGrowKeepsRowPositions(t *testing.T) {
	s := New(10, 4)
	fill(t, s, 3)
	s.Resize(10, 6)

	lines := s.Lines()
	if lines[0] != "L1" || lines[1] != "L2" || lines[2] != "L3" {
		t.Fatalf("a grow moved the content:\n%s", s.Dump())
	}
	if lines[4] != "" || lines[5] != "" {
		t.Fatalf("a grow must add blank rows at the bottom:\n%s", s.Dump())
	}
	if top, bot := s.Margins(); top != 1 || bot != 6 {
		t.Fatalf("margins after resize = %d..%d, want 1..6", top, bot)
	}
}

func TestResizeNonReflowShrinkDropsTopIntoScrollback(t *testing.T) {
	s := New(10, 4)
	fill(t, s, 4)
	s.Resize(10, 2)

	lines := s.Lines()
	if lines[0] != "L3" || lines[1] != "L4" {
		t.Fatalf("a shrink must keep the BOTTOM of the content:\n%s", s.Dump())
	}
	if sb := s.Scrollback(); len(sb) == 0 || sb[len(sb)-1] != "L2" {
		t.Fatalf("a shrink must push the dropped rows into scrollback, got %q", sb)
	}
	for _, row := range []string{"L1", "L2", "L3", "L4"} {
		if n := strings.Count(s.AllText(), row); n != 1 {
			t.Fatalf("%s appears %d times across scrollback+screen, want 1:\n%s", row, n, s.AllText())
		}
	}
}

func TestResizeBottomAnchoredGrowPullsRowsBackInView(t *testing.T) {
	s := New(10, 4)
	s.SetResizeMode(ResizeBottomAnchored)
	fill(t, s, 8)
	before := s.Scrollback()
	if len(before) == 0 {
		t.Fatalf("fixture: expected overflow in scrollback, screen:\n%s", s.Dump())
	}
	s.Resize(10, 6)

	lines := s.Lines()
	if lines[len(lines)-1] != "L8" {
		t.Fatalf("bottom-anchored content must stay on the last row:\n%s", s.Dump())
	}
	// The grow shifts the content down by 2, so the two rows the earlier
	// scrolls pushed out come back at the top of the screen.
	if lines[0] != "L3" {
		t.Fatalf("a grow must pull the hidden rows back into view, top row = %q:\n%s", lines[0], s.Dump())
	}
	for _, row := range []string{"L1", "L8"} {
		if n := strings.Count(s.AllText(), row); n != 1 {
			t.Fatalf("%s appears %d times across scrollback+screen, want 1:\n%s", row, n, s.AllText())
		}
	}
}

func TestResizeReflowRewrapsAtTheNewWidth(t *testing.T) {
	s := New(20, 5)
	s.SetResizeMode(ResizeReflow)
	if _, err := s.WriteString("ABCDEFGHIJKLMNOPQRSTUVWXYZ\nsecond\n"); err != nil {
		t.Fatal(err)
	}
	if !s.WrappedPastEdge() {
		t.Fatal("fixture: the 26-column line should have auto-wrapped")
	}
	s.Resize(10, 5)

	text := s.AllText()
	if !strings.Contains(text, "ABCDEFGHIJ\nKLMNOPQRST") {
		t.Fatalf("the rewrapped halves are missing or wrongly joined:\n%s\n%s", s.Dump(), text)
	}
	if strings.Contains(text, "KLMNOPQRSTU") {
		t.Fatalf("the line was not re-wrapped to the new width:\n%s", s.Dump())
	}
	if i, j := strings.Index(text, "QRST"), strings.Index(text, "second"); j < i {
		t.Fatalf("the reflow reordered the content:\n%s", text)
	}
	if n := strings.Count(text, "QRST"); n != 1 {
		t.Fatalf("reflow duplicated a row (QRST x%d):\n%s", n, text)
	}
}

func TestResizeReflowShrinkDropsRowsOnce(t *testing.T) {
	s := New(10, 6)
	s.SetResizeMode(ResizeReflow)
	fill(t, s, 4)
	s.Resize(10, 2)

	if got := s.Lines(); got[len(got)-1] != "L4" {
		t.Fatalf("the newest row must stay visible:\n%s", s.Dump())
	}
	for _, row := range []string{"L1", "L2", "L3", "L4"} {
		if n := strings.Count(s.AllText(), row); n != 1 {
			t.Fatalf("%s appears %d times across scrollback+screen, want 1:\n%s", row, n, s.AllText())
		}
	}
}

func TestRegionScrollCapturesScrollback(t *testing.T) {
	s := New(10, 4)
	if _, err := s.WriteString("\x1b[1;3r"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteString("A\nB\nC\nD\n"); err != nil {
		t.Fatal(err)
	}
	if top, bot := s.Margins(); top != 1 || bot != 3 {
		t.Fatalf("margins = %d..%d, want 1..3", top, bot)
	}
	if sb := s.Scrollback(); len(sb) == 0 || sb[0] != "A" {
		t.Fatalf("a region scroll must fill scrollback, got %q", sb)
	}
}

// A DECSTBM region that excludes row 1 has nowhere to send a scrolled-out
// line: a real terminal discards it. The fixture models that, so a test can
// tell "erased" apart from "scrolled out of view".
func TestRegionScrollOutsideRowOneDiscards(t *testing.T) {
	s := New(10, 4)
	if _, err := s.WriteString("KEEP\x1b[2;3r\x1b[2;1HA\nB\nC\n"); err != nil {
		t.Fatal(err)
	}
	if len(s.Scrollback()) != 0 {
		t.Fatalf("a region that excludes row 1 discards rather than stores: %q", s.Scrollback())
	}
	if n := strings.Count(s.AllText(), "A"); n != 0 {
		t.Fatalf("the discarded row survived into AllText:\n%s", s.AllText())
	}
	if s.Line(1) != "KEEP" {
		t.Fatalf("a region scroll must not touch rows above the region:\n%s", s.Dump())
	}
}
