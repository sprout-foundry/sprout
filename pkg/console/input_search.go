package console

import (
	"fmt"
	"strings"
)

// ─── SP-048-4e: Ctrl-R reverse search methods ───────────────────────────────

// enterSearchMode starts a new reverse-history search, saving the current
// line/cursor so they can be restored on cancellation.
func (ir *InputReader) enterSearchMode() {
	ir.searchMode = true
	ir.searchQuery = ""
	ir.searchResult = ""
	ir.searchResultIndex = -1
	ir.preSearchLine = ir.line
	ir.preSearchCursorPos = ir.cursorPos
	// Show most recent history entry for empty query.
	if len(ir.history) > 0 {
		ir.searchResult = ir.history[len(ir.history)-1]
		ir.searchResultIndex = len(ir.history) - 1
	}
}

// exitSearchMode leaves reverse-search mode.  When accept is true the
// current searchResult is loaded into the line buffer; otherwise the
// pre-search state is restored.
func (ir *InputReader) exitSearchMode(accept bool) {
	if accept && ir.searchResult != "" {
		ir.line = ir.searchResult
		ir.cursorPos = len(ir.searchResult)
		ir.hasEditedLine = true
		ir.historyIndex = -1
	} else {
		ir.line = ir.preSearchLine
		ir.cursorPos = ir.preSearchCursorPos
	}
	ir.searchMode = false
	ir.searchQuery = ""
	ir.searchResult = ""
	ir.searchResultIndex = -1
	ir.preSearchLine = ""
	ir.preSearchCursorPos = 0
}

// handleSearchByte processes a single byte while in search mode.
func (ir *InputReader) handleSearchByte(b byte) {
	// TODO(SP-048-4e): This function processes typed input one byte at a time.
	// For ASCII this works fine, but multi-byte UTF-8 sequences (e.g. accented
	// characters, CJK) will be split across multiple handleSearchByte calls.
	// Each byte ≥ 128 is converted via string(rune(b)) which produces an
	// incorrect code point — e.g. 0xC3 becomes 'Ã' (U+00C3) instead of being
	// part of a multi-byte sequence like 'é'. Properly fixing this requires
	// buffering bytes until a complete UTF-8 rune is available via utf8.DecodeRune.
	switch {
	case b == 127 || b == 8: // Backspace
		if len(ir.searchQuery) > 0 {
			ir.searchQuery = ir.searchQuery[:len(ir.searchQuery)-1]
		}
		// Re-search from the end of history (find newest match).
		if ir.searchQuery == "" {
			// Empty query: show most recent entry again.
			if len(ir.history) > 0 {
				ir.searchResult = ir.history[len(ir.history)-1]
				ir.searchResultIndex = len(ir.history) - 1
			} else {
				ir.searchResult = ""
				ir.searchResultIndex = -1
			}
		} else {
			result, idx, ok := ir.searchHistory(ir.searchQuery, len(ir.history)-1)
			if ok {
				ir.searchResult = result
				ir.searchResultIndex = idx
			} else {
				ir.searchResult = ""
				ir.searchResultIndex = -1
			}
		}
	case b >= 32: // Printable character
		ir.searchQuery += string(rune(b))
		// Search from the end of history (newest first).
		result, idx, ok := ir.searchHistory(ir.searchQuery, len(ir.history)-1)
		if ok {
			ir.searchResult = result
			ir.searchResultIndex = idx
		} else {
			ir.searchResult = ""
			ir.searchResultIndex = -1
		}
		// Everything else: ignore
}}

// cycleSearchResult searches for the next older match with the current query.
//
// When searchResultIndex is -1 (no previous match found), searchHistory
// normalizes the negative startIndex (searchResultIndex-1 = -2) back to
// len(history)-1, effectively wrapping around to search from the end again.
// In practice this means cycling when already at "no match" is a no-op,
// which is the desired behavior.
func (ir *InputReader) cycleSearchResult() {
	if ir.searchQuery == "" {
		// No query: cycle through history in order.
		if len(ir.history) > 0 {
			idx := ir.searchResultIndex - 1
			if idx < 0 {
				idx = len(ir.history) - 1
			}
			ir.searchResult = ir.history[idx]
			ir.searchResultIndex = idx
		}
		return
	}
	result, idx, ok := ir.searchHistory(ir.searchQuery, ir.searchResultIndex-1)
	if ok {
		ir.searchResult = result
		ir.searchResultIndex = idx
	}
	// If not found: keep current result (user can keep pressing Ctrl-R but
	// it just stays on the current match).
}

// searchHistory searches ir.history backwards from startIndex for a case-insensitive
// substring match of query.  Returns the matching entry, its index, and whether
// a match was found.
func (ir *InputReader) searchHistory(query string, startIndex int) (string, int, bool) {
	if startIndex < 0 {
		startIndex = len(ir.history) - 1
	}
	if len(ir.history) == 0 {
		return "", -1, false
	}
	queryLower := strings.ToLower(query)
	for i := startIndex; i >= 0; i-- {
		if strings.Contains(strings.ToLower(ir.history[i]), queryLower) {
			return ir.history[i], i, true
		}
	}
	return "", -1, false
}

// renderSearchPrompt draws the reverse-search prompt line.
func (ir *InputReader) renderSearchPrompt() {
	// Clear the current line and go to the beginning.
	fmt.Printf("\r%s", ClearLineSeq())

	// Replace newlines with "\n" to prevent multi-line terminal rendering issues
	// when the history entry itself contains newline characters.
	display := strings.ReplaceAll(ir.searchResult, "\n", "\\n")

	if ir.searchResult != "" {
		fmt.Printf("(reverse-i-search)'%s': %s", BoldText(ir.searchQuery), display)
	} else {
		// No match found.
		fmt.Printf("(failing reverse-i-search)'%s': ", BoldText(ir.searchQuery))
	}

	// Clear any trailing content.
	fmt.Printf("%s", ClearToEndOfLineSeq())
}
