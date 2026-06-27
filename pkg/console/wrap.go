package console

import (
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
)

// WrapHardLine splits a single line of text into visual rows that fit within
// `cols` terminal columns, breaking on rune boundaries and accounting for
// wide (CJK) runes via runewidth. An empty input yields one empty row so
// hard-line counts map cleanly to visual-line counts.
//
// Unlike strings.Split, this is width-aware: a rune that won't fit in the
// remaining columns starts a new row, matching how the terminal renders it.
// Combining runes (width 0) are accumulated without breaking.
func WrapHardLine(s string, cols int) []string {
	if cols <= 0 {
		return []string{s}
	}
	var rows []string
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > cols && w > 0 {
			rows = append(rows, b.String())
			b.Reset()
			w = 0
		}
		b.WriteRune(r)
		w += rw
	}
	rows = append(rows, b.String())
	return rows
}

// WrapSteerLayout is the width-aware layout helper for the steer panel.
// It takes a steer text (already includes any prefix) and a byte cursor
// position within it, and returns:
//   - lines: visual rows after hard-break (\n) split + soft wrap (cols)
//   - cursorRow, cursorCol: 0-based (row, col) within `lines` for the
//     given byte cursor position
//
// When the total visual row count exceeds maxRows, the topmost rows are
// dropped and the first visible row is prefixed with "… " so the caret
// row stays visible. cursorRow is clamped into the visible range and
// cursorCol is clamped into [0, len(lines[cursorRow])].
//
// The mapping walks `text` once with the same wrap semantics as
// WrapHardLine + \n-as-hard-break, so byte→(row,col) is exact for any
// byte index within the string. This is the same model WrappedGeometry
// in input_render.go uses (with explicit columns), but bounded by maxRows.
func WrapSteerLayout(text string, cursorByte, cols, maxRows int) (lines []string, cursorRow, cursorCol int) {
	rows, rowOfCursor, colOfCursor := splitWrappedLines(text, cols, cursorByte)
	// Cap rows.
	if maxRows > 0 && len(rows) > maxRows {
		drop := len(rows) - maxRows
		visible := make([]string, maxRows)
		copy(visible, rows[drop:])
		visible[0] = "… " + visible[0]
		rows = visible
		if rowOfCursor < drop {
			rowOfCursor = 0
			colOfCursor = 0
		} else {
			rowOfCursor -= drop
		}
	}
	if rowOfCursor < len(rows) && colOfCursor > len(rows[rowOfCursor]) {
		colOfCursor = len(rows[rowOfCursor])
	}
	if rowOfCursor < 0 {
		rowOfCursor = 0
	}
	return rows, rowOfCursor, colOfCursor
}

// splitWrappedLines is the internal byte→(row,col) walker used by
// WrapSteerLayout. It treats '\n' as a hard break (next row, col 0) and
// applies width-aware soft wrap to each hard line. Returns the full
// visual-line array plus the (row, col) for cursorByte.
func splitWrappedLines(text string, cols, cursorByte int) (lines []string, cursorRow, cursorCol int) {
	cursorRow, cursorCol = -1, 0
	byteIdx := 0
	for {
		hardEnd := strings.IndexByte(text[byteIdx:], '\n')
		var hardLine string
		if hardEnd < 0 {
			hardLine = text[byteIdx:]
		} else {
			hardLine = text[byteIdx : byteIdx+hardEnd]
		}
		hardLineStart := byteIdx

		wrapped := WrapHardLine(hardLine, cols)
		// For each wrapped row in this hard line, find which byte range
		// it covers and decide whether the cursor falls in it.
		rowStartByteInHard := 0
		for ri, rowText := range wrapped {
			rowStartByteInText := hardLineStart + rowStartByteInHard
			rowEndByteInText := rowStartByteInText + len(rowText)
			if cursorRow == -1 && cursorByte >= rowStartByteInText && cursorByte <= rowEndByteInText {
				cursorRow = len(lines) + ri
				cursorCol = cursorByte - rowStartByteInText
			}
			rowStartByteInHard += len(rowText)
		}
		lines = append(lines, wrapped...)

		if hardEnd < 0 {
			break
		}
		// Cursor sits exactly on the \n separator — place it at end of
		// the current hard line's last wrapped row.
		if cursorRow == -1 && cursorByte == hardLineStart+hardEnd {
			cursorRow = len(lines) - 1
			if cursorRow < 0 {
				cursorRow = 0
			}
			cursorCol = len(wrapped[len(wrapped)-1])
		}
		byteIdx += hardEnd + 1
	}
	if cursorRow == -1 {
		if len(lines) == 0 {
			lines = []string{""}
		}
		cursorRow = len(lines) - 1
		cursorCol = len(lines[cursorRow])
	}
	return lines, cursorRow, cursorCol
}

// WriteWrappedLines writes `lines` to `w`, each padded to `cols` columns
// with spaces (or truncated with "…" if over cols). No trailing newline —
// the caller controls layout.
//
// When withCursorRow >= 0, a visible caret (▏) is inserted into
// lines[withCursorRow] at byte column cursorCol. The caret counts against
// the cols budget, so the surrounding text is truncated if needed.
func WriteWrappedLines(w io.Writer, lines []string, cols, withCursorRow, cursorCol int) error {
	const caret = "▏"
	for i, line := range lines {
		body := line
		if i == withCursorRow && withCursorRow >= 0 {
			caretLen := visibleLen(caret)
			cc := cursorCol
			if cc < 0 {
				cc = 0
			}
			if cc > len(body) {
				cc = len(body)
			}
			if visibleLen(body)+caretLen >= cols {
				body = truncWithEllipsis(body, cols-caretLen-1)
				if cc > len(body) {
					cc = len(body)
				}
			}
			body = body[:cc] + caret + body[cc:]
		} else if visibleLen(body) >= cols {
			body = truncWithEllipsis(body, cols-1)
		}
		pad := cols - visibleLen(body)
		if pad < 0 {
			pad = 0
		}
		if _, err := io.WriteString(w, body); err != nil {
			return err
		}
		if pad > 0 {
			if _, err := io.WriteString(w, strings.Repeat(" ", pad)); err != nil {
				return err
			}
		}
	}
	return nil
}