package console

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newShiftTestFooter(buf *strings.Builder, rows int) *StatusFooter {
	return &StatusFooter{
		w: buf, isTTY: true, active: true, steerCursor: -1,
		sizeOverride: &terminalSizeOverride{cols: 80, rows: rows},
	}
}

func TestShiftScrollRegion_GrowScrollsContentUp(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.applyScrollRegionLocked() // margins 1..22
	buf.Reset()

	f.steerActive, f.steerLine = true, "a\nb\nc" // box grows by 3 rows + its rule
	f.shiftScrollRegionLocked()

	require.Equal(t, "\033[22;1H\n\n\n\n", buf.String(),
		"line feeds on the old bottom margin push the transcript up instead of under the block")
}

func TestShiftScrollRegion_ShrinkScrollsContentBackDown(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.steerActive, f.steerLine = true, "a\nb\nc"
	f.applyScrollRegionLocked() // margins 1..18
	buf.Reset()

	f.steerActive, f.steerLine = false, ""
	f.shiftScrollRegionLocked()

	require.Equal(t, "\033[1;22r\033[1;1H\033M\033M\033M\033M", buf.String(),
		"widen the margins, then reverse-index so the vacated rows fall off the bottom")
}

func TestApplyScrollRegionKeepingCursor_FollowsTheLiveLine(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.applyScrollRegionLocked() // margins 1..22
	buf.Reset()

	f.steerActive, f.steerLine = true, "a" // steer box (row + rule) appears mid-turn
	f.applyScrollRegionKeepingCursorLocked()
	require.Equal(t, "\0337\033[1;22r\033[22;1H\n\n\0338\033[2A\0337\033[1;20r\0338", buf.String(),
		"scroll up by the box height, follow the line up under the old margins, then set the new ones around a re-saved cursor")

	buf.Reset()
	f.steerActive, f.steerLine = false, ""
	f.applyScrollRegionKeepingCursorLocked()
	require.Equal(t, "\0337\033[1;22r\033[1;1H\033M\033M\0338\033[2B", buf.String(),
		"shrinking scrolls back down and the cursor follows")
}

func TestShiftScrollRegion_NoOpWithoutPriorRegionOrAfterResize(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.steerActive, f.steerLine = true, "a"
	f.shiftScrollRegionLocked()
	require.Empty(t, buf.String(), "nothing applied yet: no content to move")

	f.applyScrollRegionLocked()
	buf.Reset()
	f.sizeOverride.rows = 30
	f.steerActive = false
	f.shiftScrollRegionLocked()
	require.Empty(t, buf.String(), "the resize path owns a height change")
}
