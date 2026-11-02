package console

// caret_rows.go — moving the input caret by visual rows: Up/Down through a
// wrapped or multi-line input, and Page Up/Page Down by a screenful.
//
// Each input box has its own layout — the inline prompt wraps where the
// terminal does (by character, after the prompt), the pinned box by words at
// composerWrapCols — so each produces its caret positions from that layout,
// and the movement itself is shared.

import (
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// caretPos is a byte offset in the input and the row and screen column the
// caret renders at when it sits there.
type caretPos struct{ b, row, col int }

// inlineCaretPositions lists the caret positions of the inline prompt, using
// wrappedGeometry's rules: a byte where the terminal soft-wraps renders at
// the end of the earlier row.
func inlineCaretPositions(content string, cols, promptWidth int) []caretPos {
	var out []caretPos
	row, col := 0, promptWidth
	for i := 0; i < len(content); {
		out = append(out, caretPos{i, row, col})
		r, size := utf8.DecodeRuneInString(content[i:])
		if r == '\n' {
			row, col = row+1, 0
		} else {
			w := runewidth.RuneWidth(r)
			if cols > 0 && col+w > cols {
				row, col = row+1, 0
			}
			col += w
		}
		i += size
	}
	return append(out, caretPos{len(content), row, col})
}

// composerCaretPositions lists the caret positions of the pinned input box's
// word-wrapped layout (see composerCaret). A soft-wrap boundary is listed for
// both rows; caretRowMove resolves it to the earlier row, as rendering does.
func composerCaretPositions(text string, cols int) []caretPos {
	lines, _, _ := splitWrappedLines(text, cols, -1)
	var out []caretPos
	b := 0
	for ri, row := range lines {
		col := 0
		for i, r := range row {
			out = append(out, caretPos{b + i, ri, col})
			col += runewidth.RuneWidth(r)
		}
		b += len(row)
		out = append(out, caretPos{b, ri, col})
		if b < len(text) && text[b] == '\n' {
			b++
		}
	}
	return out
}

// caretRowMove moves the caret at byte cur by delta rows, keeping its screen
// column as near as the target row allows. With clamp, a move past the first
// or last row lands on that row; without it, the move fails (ok false) when
// the caret is already on the edge row in the direction of travel — the
// signal for Up/Down to fall through to history.
func caretRowMove(positions []caretPos, cur, delta int, clamp bool) (int, bool) {
	if len(positions) == 0 || delta == 0 {
		return cur, false
	}
	// A byte's row is its first listing: the earlier row at a soft wrap.
	rowOf := make(map[int]int, len(positions))
	lastRow := 0
	for _, p := range positions {
		if _, seen := rowOf[p.b]; !seen {
			rowOf[p.b] = p.row
		}
		lastRow = max(lastRow, p.row)
	}
	curRow, curCol := 0, 0
	for _, p := range positions {
		if p.b == cur {
			curRow, curCol = p.row, p.col
			break
		}
		if p.b > cur {
			break
		}
		curRow, curCol = p.row, p.col
	}
	target := curRow + delta
	if target < 0 || target > lastRow {
		if !clamp || (target < 0 && curRow == 0) || (target > lastRow && curRow == lastRow) {
			return cur, false
		}
		target = min(max(target, 0), lastRow)
	}
	best, bestDist := -1, 0
	for _, p := range positions {
		if p.row != target || rowOf[p.b] != target {
			continue
		}
		d := p.col - curCol
		if d < 0 {
			d = -d
		}
		if best < 0 || d < bestDist || (d == bestDist && p.col <= curCol) {
			best, bestDist = p.b, d
		}
	}
	if best < 0 {
		return cur, false
	}
	return best, true
}
