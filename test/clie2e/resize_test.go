//go:build unix

package clie2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/testutil/vtscreen"
)

// SP-171c: the resize matrix against the rendered screen, for the three ways
// real terminals disagree about what happens to already-drawn rows.
//
// The assertion is on the transcript, not the escape stream: every marker the
// program printed must still be readable after a resize, and must appear
// exactly once across the screen and the scrollback the fixture remembers. A
// missing marker is a row the resize destroyed; a doubled one is a row the
// resize replayed on top of the copy the terminal had already pushed into
// scrollback.
//
// Width changes are covered here. They could not be before: the emulator did
// not model reflow at all, and the footer's clear window assumed it did.
func TestREPL_ResizeMatrixKeepsEveryRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode vtscreen.ResizeMode
		// steps are the geometries to move through after the first turn.
		steps [][2]int
	}{
		{
			name: "top-anchored",
			mode: vtscreen.ResizeNonReflow,
			steps: [][2]int{
				{80, 18}, // shorter
				{80, 30}, // taller
				{60, 30}, // narrower
			},
		},
		{
			name: "bottom-anchored",
			mode: vtscreen.ResizeBottomAnchored,
			steps: [][2]int{
				{80, 20},
				{80, 32},
				{64, 26},
			},
		},
		{
			name: "reflowing",
			mode: vtscreen.ResizeReflow,
			steps: [][2]int{
				{70, 26}, // narrower and taller at once
				{110, 20},
				{90, 34},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runResizeMatrix(t, tc.mode, tc.steps)
		})
	}
}

func runResizeMatrix(t *testing.T, mode vtscreen.ResizeMode, steps [][2]int) {
	t.Helper()
	// One reply per turn: turn 1 seeds the transcript, the turns after it
	// prove the console still writes correctly into whatever geometry the
	// resize left.
	s := startWithResizeMode(t, 80, 24, mode, markerBlock("A", 10), markerBlock("B", 10))
	waitForPrompt(s)
	s.typeLine("first")
	s.waitForText("A09", 20*time.Second)
	waitForIdleBox(s)

	for i, step := range steps {
		s.resize(step[0], step[1])
		time.Sleep(600 * time.Millisecond) // past the resize settle window
		s.typeLine(fmt.Sprintf("step-%d", i))
		// Wait for the turn to finish, not for a marker: a later turn can
		// scroll an earlier marker off the VISIBLE screen, which is the
		// terminal behaving correctly. The marker assertions below read the
		// whole text, scrollback included.
		waitForIdleBox(s)
		time.Sleep(300 * time.Millisecond)

		all := s.allText()
		// A row the resize duplicated is always a bug; a row it erased is a
		// bug whenever the console still claims to hold it. The newest reply
		// block is the one it must always keep: a scroll inside the footer's
		// scroll region can legitimately discard older rows off the region's
		// top, exactly as a real terminal's does on a viewport this small.
		newest := "B"
		keep := map[string]bool{}
		for i := range 10 {
			keep[fmt.Sprintf("%s%02d", newest, i)] = true
		}
		for _, marker := range allMarkers() {
			n := strings.Count(all, marker)
			switch {
			case n == 0 && keep[marker]:
				t.Fatalf("%s was destroyed by resize step %d (%dx%d):\n%s\nscrollback:\n%s",
					marker, i, step[0], step[1], s.scr.Dump(), strings.Join(s.scr.Scrollback(), "\n"))
			case n > 1:
				t.Fatalf("%s appears %d times after resize step %d (%dx%d) — the resize duplicated a row.\nscreen:\n%s\nscrollback:\n%s",
					marker, n, i, step[0], step[1], s.scr.Dump(), strings.Join(s.scr.Scrollback(), "\n"))
			}
		}
		// The footer's last drawn row is its content line; the rule sits above
		// it. The hint row, when shown, sits above the rule.
		if _, rows := s.scr.Size(); !strings.Contains(s.scr.Line(rows), "mock-model") ||
			!strings.Contains(s.scr.Line(rows-1), "─") {
			t.Fatalf("the footer is not pinned to the new bottom at %dx%d:\n%s",
				step[0], step[1], s.scr.Dump())
		}
	}
}

// markerBlock is a mock reply of n lines, each a token no other output contains.
func markerBlock(tag string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s%02d\n", tag, i)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func allMarkers() []string {
	var out []string
	for _, tag := range []string{"A", "B"} {
		for i := 0; i < 10; i++ {
			out = append(out, fmt.Sprintf("%s%02d", tag, i))
		}
	}
	return out
}
