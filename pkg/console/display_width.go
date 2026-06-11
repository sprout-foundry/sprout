package console

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// displayWidth returns the number of terminal columns a string occupies, after
// stripping ANSI escape sequences. Wide (CJK) runes count as 2 columns and
// zero-width / combining runes as 0, via go-runewidth. For pure-ASCII input
// this equals the rune count, so callers that previously measured by rune count
// are unaffected on the common path — only wide/multibyte text is corrected.
func displayWidth(s string) int {
	return runewidth.StringWidth(stripANSIEscapeCodes(s))
}

// truncateToWidth clamps s to at most maxCols terminal columns, appending
// ellipsis when it has to cut. It cuts on rune boundaries and accounts for wide
// runes, so it never splits a multibyte rune or overflows the column budget.
// The input is assumed to be plain text (no ANSI); width is measured on its
// runes directly.
func truncateToWidth(s string, maxCols int, ellipsis string) string {
	if maxCols <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxCols {
		return s
	}
	budget := maxCols - runewidth.StringWidth(ellipsis)
	if budget < 0 {
		budget = 0
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > budget {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + ellipsis
}

// padToWidth right-pads s with spaces to exactly cols display columns (or
// truncates it if it's wider). Useful for column alignment with wide-rune-safe
// measurement. Plain text only.
func padToWidth(s string, cols int) string {
	w := runewidth.StringWidth(s)
	if w == cols {
		return s
	}
	if w > cols {
		return truncateToWidth(s, cols, "")
	}
	return s + strings.Repeat(" ", cols-w)
}
