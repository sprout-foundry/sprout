package console

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/testutil/vtscreen"
	"github.com/stretchr/testify/require"
)

// newTeardownTestFooter builds a live footer with a pinned steer panel and
// the keymap hint row, the state a turn runs in.
func newTeardownTestFooter(buf *strings.Builder, rows int) *StatusFooter {
	f := &StatusFooter{
		w: buf, isTTY: true, active: true, steerCursor: -1,
		sizeOverride: &terminalSizeOverride{cols: 80, rows: rows},
	}
	f.SetTurnActive(true)
	f.SetSteerLine("> ")
	return f
}

// blankedRows returns the absolute row numbers the output clears.
func blankedRows(out string) map[int]bool {
	cleared := map[int]bool{}
	const clearToEOL = ";1H\033[K"
	for _, seg := range strings.Split(out, clearToEOL) {
		// Each match is preceded by "\033[<row>", so the row number is the
		// trailing run of digits in the segment before it.
		i := len(seg)
		for i > 0 && seg[i-1] >= '0' && seg[i-1] <= '9' {
			i--
		}
		if i == len(seg) || i == 0 || seg[i-1] != '[' {
			continue
		}
		row := 0
		for _, r := range seg[i:] {
			row = row*10 + int(r-'0')
		}
		if row > 0 {
			cleared[row] = true
		}
	}
	return cleared
}

// TestClearSteerLine_KeepsTranscriptRows is the regression for a reply cut
// short at turn end: the block shrank by more rows than it blanked, and the
// blanking landed on rows that belonged to the transcript.
func TestClearSteerLine_KeepsTranscriptRows(t *testing.T) {
	var buf strings.Builder
	f := newTeardownTestFooter(&buf, 24)

	// The steer block is the rule plus its text rows; the transcript's last
	// line sits on the region's last row.
	before := f.reservedRows()
	require.Equal(t, 4, before, "steer text row + rule + content line")

	buf.Reset()
	f.ClearSteerLine()

	after := f.reservedRows()
	require.Equal(t, 2, after, "rule + content line")

	// The block contracted by two rows. The margins were 1..20, so rows
	// 1..20 hold transcript and must never be blanked; only rows the old
	// block pinned may be.
	oldBottom := 24 - before
	for row := range blankedRows(buf.String()) {
		require.Greater(t, row, oldBottom,
			"row %d is transcript and must not be cleared", row)
	}
}

// TestClearSteerLine_RepaintsPinnedRows checks the vacated row is repainted
// rather than left holding the old steer text.
func TestClearSteerLine_RepaintsPinnedRows(t *testing.T) {
	var buf strings.Builder
	f := newTeardownTestFooter(&buf, 24)

	buf.Reset()
	f.ClearSteerLine()

	out := buf.String()
	require.NotContains(t, out, "\033[r",
		"dropping the margins mid-teardown reflows the transcript")
	require.Contains(t, out, "\033[24;1H",
		"the content line is repainted on the last row")
	require.NotContains(t, out, "> ",
		"the steer text is gone from the pinned rows")
}

// TestClearSteerLine_BlanksVacatedSteerRows pins what is on screen after
// the clear. 24 rows, steer block 4: the steer rule on 21, the steer text on
// 22, the footer rule on 23, the content line on 24. Afterwards the margins
// are 1..22, so 21 and 22 are scrolling rows that still hold chrome — left
// alone, the steer rule scrolls up through the transcript as a stray
// separator — while 23 stays the footer's rule.
func TestClearSteerLine_BlanksVacatedSteerRows(t *testing.T) {
	scr := vtscreen.New(80, 24)
	f := &StatusFooter{
		w: scr, isTTY: true, active: true, steerCursor: -1,
		sizeOverride: &terminalSizeOverride{cols: 80, rows: 24},
	}
	f.SetTurnActive(true)
	f.SetSteerLine("> draft")
	require.Contains(t, scr.Line(22), "draft")

	f.ClearSteerLine()

	require.Empty(t, scr.Line(21), "the vacated steer rule row is blanked")
	require.Empty(t, scr.Line(22), "the vacated steer text row is blanked")
	require.Equal(t, strings.Repeat("─", 80), scr.Line(23),
		"the footer's own rule survives the clear")
}

// TestClearSteerLine_BlanksWhileStreaming covers the mid-stream teardown,
// where the full draw is suppressed and the blank is the only thing that
// removes the steer text.
func TestClearSteerLine_BlanksWhileStreaming(t *testing.T) {
	var buf strings.Builder
	f := newTeardownTestFooter(&buf, 24)
	f.mu.Lock()
	f.proseStreaming = true
	f.mu.Unlock()

	buf.Reset()
	f.ClearSteerLine()

	require.True(t, blankedRows(buf.String())[22],
		"the vacated steer text row is blanked with prose streaming")
}
