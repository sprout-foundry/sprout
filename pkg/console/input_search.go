package console

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ─── SP-048-4e: Ctrl-R reverse search methods ───────────────────────────────

// enterSearchMode starts a new reverse-history search, saving the current
// line/cursor so they can be restored on cancellation.
func (ir *InputReader) enterSearchMode() {
	ir.searchMode = true
	ir.searchQuery = ""
	ir.searchResult = ""
	ir.searchResultIndex = -1
	ir.searchBuf = ir.searchBuf[:0]
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
	ir.searchBuf = ir.searchBuf[:0]
	ir.preSearchLine = ""
	ir.preSearchCursorPos = 0
}

// handleSearchByte processes a single byte while in search mode.
func (ir *InputReader) handleSearchByte(b byte) {
	switch {
	case b == 127 || b == 8: // Backspace
		ir.searchBuf = ir.searchBuf[:0]
		if len(ir.searchQuery) > 0 {
			// Trim one rune (not one byte) so multi-byte chars
			// aren't half-deleted.
			_, size := utf8.DecodeLastRuneInString(ir.searchQuery)
			ir.searchQuery = ir.searchQuery[:len(ir.searchQuery)-size]
		}
		ir.refreshSearchForQuery()
	case b >= 128 || len(ir.searchBuf) > 0:
		// Multi-byte UTF-8 sequence: buffer until we have a full rune.
		ir.searchBuf = append(ir.searchBuf, b)
		r, size := utf8.DecodeRune(ir.searchBuf)
		if r == utf8.RuneError && size == 1 && len(ir.searchBuf) < utf8.UTFMax {
			// Incomplete sequence — wait for more bytes.
			return
		}
		// Either a valid rune or an invalid sequence we must flush.
		ir.searchQuery += string(ir.searchBuf)
		ir.searchBuf = ir.searchBuf[:0]
		ir.refreshSearchForQuery()
	case b >= 32: // Printable ASCII
		ir.searchQuery += string(rune(b))
		ir.refreshSearchForQuery()
	}
}

// refreshSearchForQuery re-runs the history search for the current query
// and updates searchResult / searchResultIndex. For an empty query it
// shows the most recent history entry.
func (ir *InputReader) refreshSearchForQuery() {
	if ir.searchQuery == "" {
		if len(ir.history) > 0 {
			ir.searchResult = ir.history[len(ir.history)-1]
			ir.searchResultIndex = len(ir.history) - 1
		} else {
			ir.searchResult = ""
			ir.searchResultIndex = -1
		}
		return
	}
	result, idx, ok := ir.searchHistory(ir.searchQuery, len(ir.history)-1)
	if ok {
		ir.searchResult = result
		ir.searchResultIndex = idx
	} else {
		ir.searchResult = ""
		ir.searchResultIndex = -1
	}
}

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
