package console

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func withMouseClicks(t *testing.T, on bool) {
	t.Helper()
	SetMouseClickToPositionSource(func() bool { return on })
	t.Cleanup(func() { SetMouseClickToPositionSource(nil) })
}

func parseAll(t *testing.T, seq string) *InputEvent {
	t.Helper()
	ep := NewEscapeParser()
	var got *InputEvent
	for i := 0; i < len(seq); i++ {
		if ev := ep.Parse(seq[i]); ev != nil {
			got = ev
		}
	}
	require.NotNil(t, got, "%q", seq)
	return got
}

// leftClick is an SGR left-button press at a 1-based cell.
func leftClick(t *testing.T, row, col int) *InputEvent {
	return parseAll(t, fmt.Sprintf("\x1b[<0;%d;%dM", col, row))
}

func TestMouseInputModes_ClickReportingOnlyWhenOptedIn(t *testing.T) {
	withMouseClicks(t, false)
	require.Equal(t, MouseTrackingSGR, mouseInputModes(), "off by default: the wheel stays the terminal's")
	withMouseClicks(t, true)
	require.Equal(t, MouseTrackingSGR+mouseClickReportingEnable, mouseInputModes())
}

func TestEscapeParser_CursorPositionReport(t *testing.T) {
	ev := parseAll(t, "\x1b[12;40R")
	require.Equal(t, EventCursorReport, ev.Type)
	require.Equal(t, "12;40", ev.Data)
}

// A click on a cell of the steer box puts the caret on the character drawn
// there — including in a row scrolled into view and on a continuation row.
func TestSteerBox_ClickPlacesCaret(t *testing.T) {
	text := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda"
	r, scr := newSteerOnScreen(t, text)

	// Find the screen cell showing "zeta" and click its 'e'.
	row, col := -1, -1
	for y := 1; y <= 24; y++ {
		if x := strings.Index(scr.line(y), "zeta"); x >= 0 {
			row, col = y, len([]rune(scr.line(y)[:x]))+2 // 1-based, on the 'e'
			break
		}
	}
	require.Positive(t, row, "zeta on screen:\n%s", scr.dump())
	r.handleEvent(leftClick(t, row, col))
	require.Equal(t, strings.Index(text, "zeta")+1, r.cursorPos, "\n%s", scr.dump())

	// A click on the box's rule or outside the box leaves the caret alone.
	before := r.cursorPos
	r.handleEvent(leftClick(t, 1, 5))
	require.Equal(t, before, r.cursorPos)
}

// At the idle prompt the click is held until the terminal reports where the
// cursor is, which locates the prompt on screen.
func TestInputReader_ClickResolvesAgainstCursorReport(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 20
	ir.line = strings.Repeat("a", 18) + strings.Repeat("b", 20) + "cc" // rows: 18 + 20 + 2
	ir.cursorPos = len(ir.line)
	ir.currentPhysicalLine = 2 // the caret is on the prompt's third row

	out := captureStdout(t, func() { ir.HandleEvent(leftClick(t, 9, 6)) })
	require.Contains(t, out, "\x1b[6n", "asks the terminal where the cursor is")
	require.Equal(t, len(ir.line), ir.cursorPos, "nothing moves until the report arrives")

	// The cursor is on screen row 10, so the prompt starts on row 8 and the
	// click (row 9, column 6) is layout row 1, column 5: 18 + 5 bytes in.
	captureStdout(t, func() { ir.HandleEvent(&InputEvent{Type: EventCursorReport, Data: "10;3"}) })
	require.Equal(t, 23, ir.cursorPos)

	// A stray report (no click pending) is ignored.
	captureStdout(t, func() { ir.HandleEvent(&InputEvent{Type: EventCursorReport, Data: "1;1"}) })
	require.Equal(t, 23, ir.cursorPos)
}
