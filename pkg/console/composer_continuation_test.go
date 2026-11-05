package console

import (
	"bytes"
	"strings"
	"testing"
)

// SP-161d: a wrapped composer row must be visibly a continuation of the
// row above it. Without a marker the rows read as separate input lines and
// the wrap point is invisible — the user reports misspellings and
// uncertainty about where a line actually broke.
func TestDrawComposer_WrappedRowCarriesContinuationMarker(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		active:       true,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 20, rows: 24},
	}

	// A buffer long enough to wrap into three visual rows at 20 cols.
	text := ComposerPrefix(ComposerSteer) + strings.Repeat("a", 45)
	lines, _, _ := f.steerVisualLines(text, -1, 3, 20, true)
	if len(lines) < 2 {
		t.Fatalf("fixture did not wrap: got %d row(s) for %q", len(lines), text)
	}

	blockRows := len(lines) + 1
	LockOutput()
	f.drawComposerLocked(text, -1, true, blockRows, 0, 24, 20, ComposerSteer)
	UnlockOutput()

	out := buf.String()
	rows := composerRowsFromDraw(out)
	// The first parsed row is the dim rule above the box; the text rows
	// follow it.
	if len(rows) != len(lines)+1 {
		t.Fatalf("expected %d composer rows (+1 rule), parsed %d from %q", len(lines), len(rows), out)
	}
	textRows := rows[1:]

	// The label row carries no marker; every row after it does.
	if strings.Contains(textRows[0], composerContinuationMarker) {
		t.Errorf("label row must not carry the continuation marker: %q", textRows[0])
	}
	for i := 1; i < len(textRows); i++ {
		if !strings.Contains(textRows[i], composerContinuationMarker) {
			t.Errorf("continuation row %d has no marker: %q", i, textRows[i])
		}
	}

	// The marker comes out of the row's width budget, so every drawn row
	// still ends exactly at the terminal edge — a marker that pushed the
	// row past `cols` would wrap and corrupt the footer geometry.
	for i, row := range textRows {
		if got := visibleLen(row); got != 20 {
			t.Errorf("row %d is %d columns wide, want 20: %q", i, got, row)
		}
	}
}

// A single-row composer (nothing wrapped) must not grow a marker — the
// marker means "this continues the row above", and there is no row above.
func TestDrawComposer_UnwrappedRowHasNoMarker(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		active:       true,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 40, rows: 24},
	}

	text := ComposerPrefix(ComposerSteer) + "short"
	LockOutput()
	f.drawComposerLocked(text, -1, false, 2, 0, 24, 40, ComposerSteer)
	UnlockOutput()

	out := buf.String()
	if strings.Contains(out, composerContinuationMarker) {
		t.Errorf("unwrapped composer must not show a continuation marker: %q", out)
	}
}

// composerRowsFromDraw extracts the composer rows from a drawComposerLocked
// byte stream: each row is written as CSI <row>;1H CSI K <styled> reset.
// Returns the visible text of each row in draw order.
//
// Parsed by scanning for the "H\x1b[K" row terminator rather than by
// splitting on an escape prefix — the erase-to-EOL and the next row's
// cursor move both start with ESC, so a split-based parse cannot keep a
// row's body intact.
func composerRowsFromDraw(out string) []string {
	const moveToRow = "H\033[K"
	var rows []string
	rest := out
	for {
		idx := strings.Index(rest, moveToRow)
		if idx < 0 {
			break
		}
		body := rest[idx+len(moveToRow):]
		// The row ends at the next cursor move (ESC [ <n> ; 1 H). Keep the
		// remainder so the loop can find the following row.
		end := strings.Index(body, "\033[")
		if end < 0 {
			rows = append(rows, stripANSIEscapeCodes(body))
			break
		}
		rows = append(rows, stripANSIEscapeCodes(body[:end]))
		rest = body[end:]
	}
	return rows
}
