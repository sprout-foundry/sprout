package console

import (
	"strings"
	"unicode/utf8"

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

// truncateLinePreservingANSI truncates s to at most maxCols of VISIBLE
// width, keeping any ANSI escape sequences that appear in the kept prefix
// intact and never splitting a sequence mid-escape. Wide (CJK) runes are
// counted via runewidth. When the input had more visible content than the
// budget allows, the result is terminated with an ellipsis rune ("…") and,
// if the kept prefix opened a color/style escape that wasn't closed (because
// the trailing reset was cut), a trailing ColorReset is appended so the
// ellipsis — and anything printed afterwards — isn't left in a stray style.
//
// Unlike truncateDisplay (markdown_formatter.go) this preserves ANSI in the
// kept portion, so a persona badge like "\033[36m[coder]\033[0m" keeps its
// colors even when the text after it is truncated.
func truncateLinePreservingANSI(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	// Fast path: the whole string already fits.
	if displayWidth(s) <= maxCols {
		return s
	}

	var b strings.Builder
	visible := 0       // visible columns consumed so far
	colorOpen := false // an SGR color/style escape is currently active
	truncated := false
	ellipsis := "…"
	ellipsisWidth := runewidth.StringWidth(ellipsis)

	// Reserve room for the ellipsis in the visible budget so the final
	// "…" never overflows maxCols.
	budget := maxCols - ellipsisWidth

	i := 0
	for i < len(s) {
		// Detect a CSI escape sequence: ESC [ ... <final byte 0x40-0x7E>.
		// These carry zero visible width and are always preserved verbatim
		// in the kept prefix; they must never be split.
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// Find the end of the sequence (first byte in 0x40–0x7E).
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7E) {
				j++
			}
			if j < len(s) {
				j++ // include the final byte
			}
			seq := s[i:j]
			// Update color-open tracking for SGR sequences (those ending in 'm').
			if strings.HasSuffix(seq, "m") {
				colorOpen = !isSGRReset(seq)
			}
			b.WriteString(seq)
			i = j
			continue
		}

		// Decode the next rune (visible content).
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			i += size
			continue
		}
		rw := runewidth.RuneWidth(r)
		if visible+rw > budget {
			truncated = true
			break
		}
		b.WriteRune(r)
		visible += rw
		i += size
	}

	if !truncated {
		return b.String()
	}

	// Append ellipsis. If a color/style escape was left open by the cut
	// (its reset was past the truncation point), close it so the ellipsis
	// and subsequent output aren't left in a stray style.
	if colorOpen {
		return b.String() + ellipsis + ColorReset
	}
	return b.String() + ellipsis
}

// isSGRReset reports whether an SGR escape sequence (e.g. "\033[0m" or
// "\033[m") is a full reset that clears all attributes. The body between
// "\033[" and the trailing "m" is inspected: empty or "0" means reset.
func isSGRReset(seq string) bool {
	// seq looks like "\033[...m"; trim the CSI introducer and final 'm'.
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\033["), "m")
	body = strings.TrimSpace(body)
	return body == "" || body == "0"
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
