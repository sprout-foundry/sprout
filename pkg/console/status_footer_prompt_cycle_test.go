package console

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// newPromptCycleFooter builds a live footer over an emulated screen with the
// mid-turn steer panel already open, and a transcript long enough to fill the
// scroll region — the state a turn is in when it asks the user something.
func newPromptCycleFooter(t *testing.T, rows int) (*StatusFooter, *vtScreen) {
	t.Helper()
	scr := newVTScreen(60, rows)
	f := &StatusFooter{
		w: scr, isTTY: true, active: true, steerCursor: -1,
		sizeOverride: &terminalSizeOverride{cols: 60, rows: rows},
	}
	f.applyScrollRegionLocked()
	f.SetSteerLineWithCursor("", 0)
	for i := 1; i <= rows*2; i++ {
		_, _ = fmt.Fprintf(scr, "t%02d\n", i)
	}
	require.True(t, scr.cursorInRegion())
	return f, scr
}

// A mid-turn prompt pauses the steer reader (the panel closes and the region
// widens), the question and answer print on the freed rows, and resuming
// reopens the panel. The panel must lift the transcript rather than cover it,
// and leave the cursor inside the margins so the prose that follows scrolls.
func TestSteerPanel_PromptCycleKeepsTranscriptScrolling(t *testing.T) {
	f, scr := newPromptCycleFooter(t, 14)

	f.ClearSteerLine() // PauseSteer → SteerInputReader.Stop
	_, _ = fmt.Fprint(scr, "question?\n")
	_, _ = fmt.Fprint(scr, "answer\n")
	f.SetSteerLineWithCursor("", 0) // ResumeSteer → Start → renderLine

	require.True(t, scr.cursorInRegion(),
		"resume must leave the cursor inside the scroll margins; below them every line feed overwrites the last row:\n%s", scr.dump())

	for i := 1; i <= 3; i++ {
		_, _ = fmt.Fprintf(scr, "p%d\n", i)
	}
	region := scr.regionLines()
	require.Equal(t, []string{"question?", "answer", "p1", "p2", "p3", ""}, region[len(region)-6:],
		"the prompt, its answer and the prose after it all stay visible, in order:\n%s", scr.dump())
}

// Repeated prompts in one turn must not compound: each cycle lifts by the
// panel's height and no more.
func TestSteerPanel_RepeatedPromptCyclesStayInRegion(t *testing.T) {
	f, scr := newPromptCycleFooter(t, 14)
	for i := 1; i <= 3; i++ {
		f.ClearSteerLine()
		_, _ = fmt.Fprintf(scr, "q%d\n", i)
		f.SetSteerLineWithCursor("", 0)
		_, _ = fmt.Fprintf(scr, "a%d\n", i)
		require.True(t, scr.cursorInRegion(), "cycle %d:\n%s", i, scr.dump())
	}
	region := scr.regionLines()
	require.Equal(t, []string{"q1", "a1", "q2", "a2", "q3", "a3", ""}, region[len(region)-7:])
}

// Growth while the panel is already open (typing wraps, the dropdown opens)
// keeps the cursor in place — the existing no-scroll contract for keystrokes.
func TestSteerPanel_GrowWhileOpenDoesNotLift(t *testing.T) {
	f, scr := newPromptCycleFooter(t, 14)
	before := scr.r
	f.SetSteerLineWithCursor("one\ntwo", 0)
	require.Equal(t, before, scr.r, "keystroke growth must not move the live line")
}
