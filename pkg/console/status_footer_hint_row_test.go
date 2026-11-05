package console

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The hint row carries a prompt's key legend (a picker's footer, the
// security dialog's keys). It is a pinned row: the scroll region has to
// exclude it, or the next transcript line scrolls over it. These tests pin
// the row's part in the reserved-row count, which is what keeps output off
// it after an ask_user / trust prompt closes.

func TestHintOverride_ReservesRowWhenKeymapHintsAreOff(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.applyScrollRegionLocked() // 2 reserved rows: margins 1..22
	require.Equal(t, 2, f.reservedRows())
	buf.Reset()

	require.True(t, f.SetHintOverride("↑↓ select · enter confirm · esc cancel"))

	require.Equal(t, 3, f.reservedRows(),
		"the override row is part of the pinned block")
	require.Contains(t, buf.String(), "\033[21;1H",
		"the hint is painted on row 21, above the rule")
	require.Contains(t, buf.String(), "\033[1;21r",
		"the margins exclude the hint row")
	require.Equal(t, "\033[22;1H\n", firstShift(buf.String()),
		"the transcript scrolls up one row to make room instead of being painted over")
}

func TestHintOverride_ReleaseRestoresKeymapRowCount(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.SetHintOverride("y/n · Esc denies")
	require.Equal(t, 3, f.reservedRows())
	buf.Reset()

	f.SetHintOverride("")

	require.Equal(t, 2, f.reservedRows(),
		"with the keymap hints off, the row goes away with the override")
	require.Contains(t, buf.String(), "\033[1;22r",
		"the margins widen back to the two-row footer")
}

func TestHintOverride_KeepsRowWhenKeymapHintsAreOn(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.showKeymapHint = true
	require.Equal(t, 3, f.reservedRows())

	f.SetHintOverride("y/n · Esc denies")
	require.Equal(t, 3, f.reservedRows(), "the row was already reserved")

	buf.Reset()
	f.SetHintOverride("")
	require.Equal(t, 3, f.reservedRows(), "the keymap hints keep the row")
	require.NotContains(t, buf.String(), "\033[1;22r",
		"no region change when the row count did not move")
}

func TestHintOverride_ShrinkingReservedBlockLeavesTranscriptAlone(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.SetTurnActive(true)
	f.SetSteerLine("> ")
	f.SetHintOverride("↑↓ select · enter confirm")
	require.Equal(t, 5, f.reservedRows(), "steer box (2) + hint (1) + footer (2)")

	buf.Reset()
	f.SetHintOverride("")

	newBottom := 24 - f.reservedRows()
	for row := range hintRowBlankedRows(buf.String()) {
		require.Greater(t, row, newBottom,
			"row %d is transcript after the shrink and must not be cleared", row)
	}
}

// hintRowBlankedRows returns the absolute row numbers the output clears.
func hintRowBlankedRows(out string) map[int]bool {
	cleared := map[int]bool{}
	for _, line := range strings.Split(out, "\033[") {
		if !strings.HasSuffix(line, ";1H\033[K") {
			continue
		}
		num := strings.TrimSuffix(line, ";1H\033[K")
		row := 0
		for _, r := range num {
			if r < '0' || r > '9' {
				row = -1
				break
			}
			row = row*10 + int(r-'0')
		}
		if row > 0 {
			cleared[row] = true
		}
	}
	return cleared
}

// firstShift returns the leading cursor-position + line-feed run the grow
// path emits to scroll the transcript up by one row.
func firstShift(out string) string {
	i := strings.Index(out, "\033[22;1H")
	if i < 0 {
		return ""
	}
	rest := out[i:]
	if j := strings.Index(rest, "\n"); j >= 0 {
		return rest[:j+1]
	}
	return rest
}
