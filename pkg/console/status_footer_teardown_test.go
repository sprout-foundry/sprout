package console

import (
	"strings"
	"testing"

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

	// The block contracted by two rows, so the bottom margin moved up. The
	// margin row itself is the topmost pinned row — the footer owns it —
	// and rows below it are pinned too. Only rows above the margin are
	// transcript, and those must never be blanked.
	newBottom := 24 - after
	for row := range blankedRows(buf.String()) {
		require.GreaterOrEqual(t, row, newBottom,
			"row %d is transcript after the shrink and must not be cleared", row)
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

// TestClearSteerLine_BlanksVacatedSteerRows pins the row the steer text
// actually occupied. The full draw only clears the rows of the block it
// paints now (rule + content), so without an explicit blank the old steer
// text survives on screen above the idle prompt — and a test that only
// looks for the text anywhere in the stream still passes, because the
// setup's own draw wrote it there. This one looks at the clear's writes.
func TestClearSteerLine_BlanksVacatedSteerRows(t *testing.T) {
	var buf strings.Builder
	f := newTeardownTestFooter(&buf, 24)

	buf.Reset()
	f.ClearSteerLine()

	// 24 rows, steer block 4 (rule at 21, steer text at 22, rule at 23,
	// content at 24). After the clear the block is 2, so the bottom margin
	// moves to row 22: the vacated rows are 22 (the steer text) and 23 (the
	// old rule). Row 21 is transcript again and must be left alone.
	cleared := blankedRows(buf.String())
	require.True(t, cleared[22], "the vacated steer text row is blanked")
	require.True(t, cleared[23], "the vacated rule row is blanked")
	require.False(t, cleared[21],
		"row 21 is transcript after the shrink and must not be cleared")
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
