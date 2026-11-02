package console

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// newSteerOnScreen builds a steer reader drawing into an emulated screen, with
// a message long enough to overflow the box.
func newSteerOnScreen(t *testing.T, text string) (*SteerInputReader, *vtScreen) {
	t.Helper()
	t.Setenv("NO_COLOR", "1") // the caret is then a visible glyph
	scr := newVTScreen(40, 24)
	f := &StatusFooter{w: scr, isTTY: true, active: true, steerCursor: -1,
		sizeOverride: &terminalSizeOverride{cols: 40, rows: 24}}
	f.applyScrollRegionLocked()
	r := NewSteerInputReader(f, nil, nil, nil)
	r.mu.Lock()
	r.setLineLocked(text)
	r.mu.Unlock()
	r.renderLine()
	return r, scr
}

// boxRows returns the input box's text rows as shown (between its rule and
// the footer's rule).
func boxRows(scr *vtScreen) []string {
	var rows []string
	rule := strings.Repeat("─", 40)
	in := false
	for r := 1; r <= 24; r++ {
		line := scr.line(r)
		if line == rule {
			if in {
				break
			}
			in = true
			continue
		}
		if in {
			rows = append(rows, line)
		}
	}
	return rows
}

func caretShown(rows []string) bool {
	for _, r := range rows {
		if strings.Contains(r, caretGlyph) {
			return true
		}
	}
	return false
}

func TestSteerBox_WindowFollowsCaretThroughLongInput(t *testing.T) {
	words := make([]string, 60)
	for i := range words {
		words[i] = "w" + strings.Repeat("x", i%5)
	}
	r, scr := newSteerOnScreen(t, strings.Join(words, " "))

	rows := boxRows(scr)
	require.Len(t, rows, maxSteerRows)
	require.True(t, strings.HasPrefix(rows[0], composerMoreMarker), "rows hidden above are marked:\n%s", scr.dump())
	require.True(t, caretShown(rows))

	// Walk up row by row: the caret stays on screen the whole way.
	for i := 0; i < 20; i++ {
		r.handleEvent(&InputEvent{Type: EventUp})
		require.True(t, caretShown(boxRows(scr)), "Up %d hid the caret:\n%s", i, scr.dump())
	}
	rows = boxRows(scr)
	require.True(t, strings.HasPrefix(rows[0], SteerPromptPrefix), "the top of the message, label first:\n%s", scr.dump())
	require.True(t, strings.HasPrefix(rows[len(rows)-1], composerMoreMarker), "rows hidden below are marked")

	// Page Down to the end and back with Page Up.
	for i := 0; i < 10; i++ {
		r.handleEvent(&InputEvent{Type: EventPageDown})
	}
	require.Equal(t, len(r.line), r.cursorPos)
	r.handleEvent(&InputEvent{Type: EventPageUp})
	require.True(t, caretShown(boxRows(scr)))
	require.Less(t, r.cursorPos, len(r.line))
}

// On the first row, Up is history again — the message is not trapped.
func TestSteerBox_UpOnFirstRowRecallsHistory(t *testing.T) {
	r, _ := newSteerOnScreen(t, "")
	r.mu.Lock()
	r.history = []string{"an earlier message"}
	r.mu.Unlock()
	r.handleEvent(&InputEvent{Type: EventUp})
	require.Equal(t, "an earlier message", r.line)
}
