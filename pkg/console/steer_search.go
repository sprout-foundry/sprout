package console

import (
	"strings"
	"unicode/utf8"
)

// ─── Ctrl-R reverse-search methods (SP-048-4e parity) ──────────────────

// enterSearchMode starts a new reverse-history search, saving the current
// buffer/cursor so they can be restored on cancellation.
func (r *SteerInputReader) enterSearchMode() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.searchMode = true
	r.searchQuery = ""
	r.searchResult = ""
	r.searchResultIndex = -1
	r.searchBuf = r.searchBuf[:0]
	// Snapshot current buffer so Esc restores it.
	snap := make([]byte, len(r.buffer))
	copy(snap, r.buffer)
	r.preSearchBuffer = snap
	r.preSearchCursorPos = r.cursorPos
	// Show most recent history entry for empty query.
	if len(r.history) > 0 {
		r.searchResult = r.history[len(r.history)-1]
		r.searchResultIndex = len(r.history) - 1
	}
}

// exitSearchMode leaves reverse-search mode. When accept is true the
// current searchResult is loaded into the buffer; otherwise the
// pre-search state is restored.
func (r *SteerInputReader) exitSearchMode(accept bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if accept && r.searchResult != "" {
		r.buffer = []byte(r.searchResult)
		r.cursorPos = len(r.buffer)
		r.historyIndex = -1
		r.pendingBuffer = nil
	} else {
		r.buffer = r.preSearchBuffer
		r.cursorPos = r.preSearchCursorPos
	}
	r.searchMode = false
	r.searchQuery = ""
	r.searchResult = ""
	r.searchResultIndex = -1
	r.searchBuf = r.searchBuf[:0]
	r.preSearchBuffer = nil
	r.preSearchCursorPos = 0
}

// handleSearchBackspace trims one rune from the search query and
// re-runs the search.
func (r *SteerInputReader) handleSearchBackspace() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.searchBuf = r.searchBuf[:0]
	if len(r.searchQuery) > 0 {
		_, size := utf8.DecodeLastRuneInString(r.searchQuery)
		r.searchQuery = r.searchQuery[:len(r.searchQuery)-size]
	}
	r.refreshSearchForQueryLocked()
}

// refreshSearchForQuery re-runs the history search for the current query
// and updates searchResult / searchResultIndex. Must NOT hold r.mu.
func (r *SteerInputReader) refreshSearchForQuery() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshSearchForQueryLocked()
}

// refreshSearchForQueryLocked is the lock-free inner search. Caller
// must hold r.mu.
func (r *SteerInputReader) refreshSearchForQueryLocked() {
	if r.searchQuery == "" {
		if len(r.history) > 0 {
			r.searchResult = r.history[len(r.history)-1]
			r.searchResultIndex = len(r.history) - 1
		} else {
			r.searchResult = ""
			r.searchResultIndex = -1
		}
		return
	}
	// Search backwards through history for a case-insensitive match,
	// starting from the most recent entry.
	queryLower := strings.ToLower(r.searchQuery)
	startIdx := len(r.history) - 1
	for i := startIdx; i >= 0; i-- {
		if strings.Contains(strings.ToLower(r.history[i]), queryLower) {
			r.searchResult = r.history[i]
			r.searchResultIndex = i
			return
		}
	}
	r.searchResult = ""
	r.searchResultIndex = -1
}

// cycleSearchResult searches for the next older match with the current
// query. Called when the user presses Ctrl-R again while already in
// search mode.
func (r *SteerInputReader) cycleSearchResult() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.searchQuery == "" {
		// No query: cycle through history in order.
		if len(r.history) > 0 {
			idx := r.searchResultIndex - 1
			if idx < 0 {
				idx = len(r.history) - 1
			}
			r.searchResult = r.history[idx]
			r.searchResultIndex = idx
		}
		return
	}
	queryLower := strings.ToLower(r.searchQuery)
	startIdx := r.searchResultIndex - 1
	for i := startIdx; i >= 0; i-- {
		if strings.Contains(strings.ToLower(r.history[i]), queryLower) {
			r.searchResult = r.history[i]
			r.searchResultIndex = i
			return
		}
	}
	// No older match found — keep current result.
}
