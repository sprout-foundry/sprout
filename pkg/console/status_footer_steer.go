package console

import (
	"strings"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// ---------------------------------------------------------------------------
// steer row rendering helpers
// ---------------------------------------------------------------------------

// steerRowFor returns the absolute terminal row (1-based) where the
// i-th rendered steer line should be drawn. The rule sits at `rows-1`
// and the footer at `rows`. When a hint row is active (hintRows=1),
// it sits at `rows-2`; steer lines stack above the hint. When no hint
// is active (hintRows=0), steer lines stack directly above the rule.
//
// Examples (rows=24):
//
//	hintRows=0, steerRows=1, i=0  → row 22  (rows-1-0-1+0)
//	hintRows=1, steerRows=1, i=0  → row 21  (rows-1-1-1+0)
//	hintRows=1, steerRows=2, i=0  → row 20  (rows-1-1-2+0)
//	hintRows=1, steerRows=2, i=1  → row 21  (rows-1-1-2+1)
//
// A previous version of this calculation wrote to `rows-1-steerRows+i+1`
// (one row lower), placing the steer panel on the rule's row. The rule
// repainted on the same draw call and the panel vanished entirely from
// the terminal. SP-055.
func steerRowFor(rows, steerRows, hintRows, i int) int {
	return rows - 1 - hintRows - steerRows + i
}

// splitSteerLines breaks the steer buffer into at most `cap` lines.
// When the buffer contains more lines than the cap, the topmost line
// shown gets a leading `…` so the user sees that earlier content is
// scrolled off — the last lines (including the caret) are always
// visible so typing never goes off-screen.
func splitSteerLines(text string, cap int) []string {
	if cap <= 0 {
		return nil
	}
	all := strings.Split(text, "\n")
	if len(all) <= cap {
		return all
	}
	overflow := all[len(all)-cap:]
	overflow[0] = "… " + overflow[0]
	return overflow
}

// steerColor is the ANSI prefix for the active steer input row —
// brighter than the cyan footer chrome so the user can tell at a
// glance that this row is interactive.
const steerColor = "\033[1;96m" // bold bright-cyan

// steerRowText pads a single steer-panel row to the terminal width.
// When withCursor is true, a visible cursor caret is appended after
// the text — used only on the LAST row of a multi-row steer panel so
// the user always sees where the next keystroke will land, regardless
// of where the terminal's blinking cursor was parked by the most
// recent save/restore. Earlier rows omit the caret to stay visually
// quiet. This is the legacy caret-at-end path; callers that track a
// cursor position should use steerRowTextWithCursor instead.
func steerRowText(text string, cols int, withCursor bool) string {
	return steerRowTextWithCursor(text, cols, withCursor, -1)
}

// steerRowTextWithCursor pads a steer-panel row to the terminal width and,
// when withCursor is true, marks the caret at byte offset cursorCol (or at
// the end when cursorCol is out of range). The caret is a block cursor —
// the character under it in reverse video, a reversed space past the end —
// so it adds no width: an inserted glyph would push a full-width wrapped
// row past the edge and cost it its last character. Without color it falls
// back to inserting ▏.
func steerRowTextWithCursor(text string, cols int, withCursor bool, cursorCol int) string {
	body := text
	if visibleLen(body) > cols {
		body = truncWithEllipsis(body, cols-1)
	}
	if withCursor {
		body = placeCaret(body, cols, cursorCol)
	}
	return body + strings.Repeat(" ", max(cols-visibleLen(body), 0))
}

const (
	caretOn  = "\033[7m"
	caretOff = "\033[27m"
	// caretGlyph is the no-color caret, inserted rather than overlaid.
	caretGlyph = "▏"
)

func placeCaret(body string, cols, at int) string {
	if !envutil.ResolveColorPreference(true) {
		if at < 0 || at > len(body) {
			at = len(body)
		}
		if visibleLen(body)+1 > cols {
			body = truncWithEllipsis(body, cols-2)
			at = min(at, len(body))
		}
		return body[:at] + caretGlyph + body[at:]
	}
	if at >= 0 && at < len(body) {
		_, size := utf8.DecodeRuneInString(body[at:])
		return body[:at] + caretOn + body[at:at+size] + caretOff + body[at+size:]
	}
	if visibleLen(body) < cols {
		return body + caretOn + " " + caretOff
	}
	_, size := utf8.DecodeLastRuneInString(body)
	last := len(body) - size
	return body[:last] + caretOn + body[last:] + caretOff
}
