package console

import (
	"io"
	"os"
	"strings"
	"testing"
)

// ─── Helper ──────────────────────────────────────────────────────────────────

func newSearchIR(history []string) *InputReader {
	ir := &InputReader{
		prompt:        "> ",
		terminalWidth: 80,
	}
	ir.SetHistory(history)
	return ir
}

// captureStdoutSearch runs fn while capturing os.Stdout and returns the output.
func captureStdoutSearch(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// ─── searchHistory ───────────────────────────────────────────────────────────

func TestSearchHistory_BasicSubstring(t *testing.T) {
	ir := newSearchIR([]string{"ls -la", "echo hello", "ls -lh", "cat file.txt"})

	// Search for "ls" from the end → should find "ls -lh" at index 2
	result, idx, ok := ir.searchHistory("ls", len(ir.history)-1)
	if !ok {
		t.Fatal("expected match for 'ls', got none")
	}
	if idx != 2 {
		t.Fatalf("expected index 2 ('ls -lh'), got %d (%q)", idx, result)
	}
	if result != "ls -lh" {
		t.Fatalf("expected 'ls -lh', got %q", result)
	}

	// Search for "hello" → should find "echo hello" at index 1
	result, idx, ok = ir.searchHistory("hello", len(ir.history)-1)
	if !ok {
		t.Fatal("expected match for 'hello', got none")
	}
	if idx != 1 {
		t.Fatalf("expected index 1 ('echo hello'), got %d (%q)", idx, result)
	}

	// Search for "FILE" → case-insensitive match → "cat file.txt" at index 3
	result, idx, ok = ir.searchHistory("FILE", len(ir.history)-1)
	if !ok {
		t.Fatal("expected match for 'FILE', got none")
	}
	if idx != 3 {
		t.Fatalf("expected index 3 ('cat file.txt'), got %d (%q)", idx, result)
	}
	if result != "cat file.txt" {
		t.Fatalf("expected 'cat file.txt', got %q", result)
	}
}

func TestSearchHistory_NoMatch(t *testing.T) {
	ir := newSearchIR([]string{"ls -la", "echo hello"})

	result, idx, ok := ir.searchHistory("nonexistent", len(ir.history)-1)
	if ok {
		t.Fatalf("expected no match, got %q at index %d", result, idx)
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
	if idx != -1 {
		t.Fatalf("expected index -1, got %d", idx)
	}
}

func TestSearchHistory_EmptyHistory(t *testing.T) {
	ir := newSearchIR(nil)

	result, idx, ok := ir.searchHistory("anything", 0)
	if ok {
		t.Fatalf("expected no match on empty history, got %q", result)
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
	if idx != -1 {
		t.Fatalf("expected index -1, got %d", idx)
	}
}

func TestSearchHistory_EmptyHistory_NegativeStartIndex(t *testing.T) {
	ir := newSearchIR(nil)

	result, idx, ok := ir.searchHistory("anything", -1)
	if ok {
		t.Fatalf("expected no match on empty history with negative start, got %q", result)
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
	if idx != -1 {
		t.Fatalf("expected index -1, got %d", idx)
	}
}

func TestSearchHistory_StartIndex(t *testing.T) {
	ir := newSearchIR([]string{"aaa", "bbb", "aaa", "ccc"})

	// Search for "aaa" from index 3 → should find index 2
	_, idx, ok := ir.searchHistory("aaa", 3)
	if !ok || idx != 2 {
		t.Fatalf("searchHistory('aaa', 3): expected index 2, got ok=%v idx=%d", ok, idx)
	}

	// Search for "aaa" from index 1 → should find index 0
	_, idx, ok = ir.searchHistory("aaa", 1)
	if !ok || idx != 0 {
		t.Fatalf("searchHistory('aaa', 1): expected index 0, got ok=%v idx=%d", ok, idx)
	}

	// Search for "aaa" from index 0 → should match (index 0 contains "aaa")
	_, idx, ok = ir.searchHistory("aaa", 0)
	if !ok || idx != 0 {
		t.Fatalf("searchHistory('aaa', 0): expected index 0, got ok=%v idx=%d", ok, idx)
	}

	// Search for "aaa" from index -1 should be normalized to len(history)-1 = 3
	_, idx, ok = ir.searchHistory("aaa", -1)
	if !ok || idx != 2 {
		t.Fatalf("searchHistory('aaa', -1): expected index 2, got ok=%v idx=%d", ok, idx)
	}

	// Search for "ccc" from index 1 → should not match (only at index 3)
	_, idx, ok = ir.searchHistory("ccc", 1)
	if ok {
		t.Fatalf("searchHistory('ccc', 1): expected no match, got index %d", idx)
	}
}

func TestSearchHistory_CaseInsensitive(t *testing.T) {
	ir := newSearchIR([]string{"Hello World", "FOO BAR", "some other"})

	// "HELLO" should match "Hello World"
	result, _, ok := ir.searchHistory("HELLO", len(ir.history)-1)
	if !ok {
		t.Fatal("expected case-insensitive match for 'HELLO'")
	}
	if result != "Hello World" {
		t.Fatalf("expected 'Hello World', got %q", result)
	}

	// "foo" should match "FOO BAR"
	result, _, ok = ir.searchHistory("foo", len(ir.history)-1)
	if !ok {
		t.Fatal("expected case-insensitive match for 'foo'")
	}
	if result != "FOO BAR" {
		t.Fatalf("expected 'FOO BAR', got %q", result)
	}

	// "bar" should match "FOO BAR" (substring)
	result, _, ok = ir.searchHistory("bar", len(ir.history)-1)
	if !ok {
		t.Fatal("expected case-insensitive match for 'bar'")
	}
	if result != "FOO BAR" {
		t.Fatalf("expected 'FOO BAR', got %q", result)
	}
}

func TestSearchHistory_WildcardEdgeCases(t *testing.T) {
	ir := newSearchIR([]string{"a", "ab", "abc", "abcde"})

	// Single char match "c" should find "abcde" (newest with 'c')
	result, idx, ok := ir.searchHistory("c", len(ir.history)-1)
	if !ok || idx != 3 {
		t.Fatalf("expected index 3 ('abcde'), got ok=%v idx=%d", ok, idx)
	}

	// "e" should find "abcde"
	result, idx, ok = ir.searchHistory("e", len(ir.history)-1)
	if !ok || idx != 3 {
		t.Fatalf("expected index 3 ('abcde'), got ok=%v idx=%d", ok, idx)
	}

	// Empty query should match nothing interesting (but implementation handles empty query differently)
	result, idx, ok = ir.searchHistory("", len(ir.history)-1)
	if !ok || idx != 3 {
		t.Fatalf("empty query matched everything, first match at end: ok=%v idx=%d", ok, idx)
	}
	if result != "abcde" {
		t.Fatalf("expected 'abcde' for empty query, got %q", result)
	}
}

// ─── enterSearchMode ─────────────────────────────────────────────────────────

func TestEnterSearchMode(t *testing.T) {
	ir := newSearchIR([]string{"cmd1", "cmd2", "cmd3"})
	ir.line = "current line"
	ir.cursorPos = 5

	ir.enterSearchMode()

	// Verify searchMode is true
	if !ir.searchMode {
		t.Fatal("expected searchMode=true after enterSearchMode")
	}

	// Verify preSearchLine and preSearchCursorPos are saved
	if ir.preSearchLine != "current line" {
		t.Fatalf("expected preSearchLine='current line', got %q", ir.preSearchLine)
	}
	if ir.preSearchCursorPos != 5 {
		t.Fatalf("expected preSearchCursorPos=5, got %d", ir.preSearchCursorPos)
	}

	// Verify query is empty
	if ir.searchQuery != "" {
		t.Fatalf("expected empty searchQuery, got %q", ir.searchQuery)
	}

	// Verify empty query shows most recent history entry
	if ir.searchResult != "cmd3" {
		t.Fatalf("expected searchResult='cmd3' (most recent), got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 2 {
		t.Fatalf("expected searchResultIndex=2, got %d", ir.searchResultIndex)
	}
}

func TestEnterSearchMode_NoHistory(t *testing.T) {
	ir := newSearchIR(nil)
	ir.line = "existing line"
	ir.cursorPos = 3

	ir.enterSearchMode()

	if !ir.searchMode {
		t.Fatal("expected searchMode=true")
	}
	if ir.searchResult != "" {
		t.Fatalf("expected empty searchResult with no history, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != -1 {
		t.Fatalf("expected searchResultIndex=-1, got %d", ir.searchResultIndex)
	}
	if ir.preSearchLine != "existing line" {
		t.Fatalf("expected preSearchLine='existing line', got %q", ir.preSearchLine)
	}
}

func TestEnterSearchMode_ResetsState(t *testing.T) {
	// Verify that enterSearchMode resets state even if called when already in search mode
	ir := newSearchIR([]string{"cmd1", "cmd2"})
	ir.searchMode = true
	ir.searchQuery = "old query"
	ir.searchResult = "old result"
	ir.searchResultIndex = 99

	ir.enterSearchMode()

	if ir.searchQuery != "" {
		t.Fatalf("expected searchQuery to be reset to empty, got %q", ir.searchQuery)
	}
	if ir.searchResult != "cmd2" {
		t.Fatalf("expected searchResult reset to most recent, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 1 {
		t.Fatalf("expected searchResultIndex reset to 1, got %d", ir.searchResultIndex)
	}
}

// ─── exitSearchMode ──────────────────────────────────────────────────────────

func TestExitSearchMode_Accept(t *testing.T) {
	ir := newSearchIR([]string{"cmd1", "cmd2"})
	ir.line = "current line"
	ir.cursorPos = 5

	ir.enterSearchMode()
	ir.searchResult = "cmd2"
	ir.searchResultIndex = 1

	ir.exitSearchMode(true)

	// Verify line is set to the matched result
	if ir.line != "cmd2" {
		t.Fatalf("expected line='cmd2' after accept, got %q", ir.line)
	}

	// Verify cursorPos is at end of matched result
	if ir.cursorPos != 4 {
		t.Fatalf("expected cursorPos=4 (len of 'cmd2'), got %d", ir.cursorPos)
	}

	// Verify searchMode is false
	if ir.searchMode {
		t.Fatal("expected searchMode=false after exit")
	}

	// Verify search state is cleared
	if ir.searchQuery != "" {
		t.Fatalf("expected empty searchQuery, got %q", ir.searchQuery)
	}
	if ir.searchResult != "" {
		t.Fatalf("expected empty searchResult, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != -1 {
		t.Fatalf("expected searchResultIndex=-1, got %d", ir.searchResultIndex)
	}
	if ir.preSearchLine != "" {
		t.Fatalf("expected empty preSearchLine, got %q", ir.preSearchLine)
	}
	if ir.preSearchCursorPos != 0 {
		t.Fatalf("expected preSearchCursorPos=0, got %d", ir.preSearchCursorPos)
	}
}

func TestExitSearchMode_Accept_EmptyResult(t *testing.T) {
	// Accepting with no result should not change the line (no match found)
	ir := newSearchIR([]string{"other"})
	ir.line = "current"
	ir.cursorPos = 3

	ir.enterSearchMode()
	ir.searchQuery = "nomatch"
	ir.searchResult = ""
	ir.searchResultIndex = -1

	ir.exitSearchMode(true)

	// With empty result and accept=true, line should remain as-is (empty result not loaded)
	if ir.searchMode {
		t.Fatal("expected searchMode=false")
	}
}

func TestExitSearchMode_Cancel(t *testing.T) {
	ir := newSearchIR([]string{"cmd1", "cmd2"})
	ir.line = "original line"
	ir.cursorPos = 7

	ir.enterSearchMode()
	ir.searchQuery = "some query"
	ir.searchResult = "cmd2"

	ir.exitSearchMode(false)

	// Verify line is restored to pre-search state
	if ir.line != "original line" {
		t.Fatalf("expected line='original line' after cancel, got %q", ir.line)
	}

	// Verify cursorPos is restored
	if ir.cursorPos != 7 {
		t.Fatalf("expected cursorPos=7 after cancel, got %d", ir.cursorPos)
	}

	// Verify searchMode is false
	if ir.searchMode {
		t.Fatal("expected searchMode=false after cancel")
	}

	// Verify search state is cleared
	if ir.searchQuery != "" {
		t.Fatalf("expected empty searchQuery, got %q", ir.searchQuery)
	}
}

func TestExitSearchMode_Cancel_WithEmptyPreSearchLine(t *testing.T) {
	// Cancel with no prior content should restore empty line
	ir := newSearchIR([]string{"cmd1"})
	ir.line = ""
	ir.cursorPos = 0

	ir.enterSearchMode()
	ir.exitSearchMode(false)

	if ir.line != "" {
		t.Fatalf("expected empty line after cancel with empty pre-search, got %q", ir.line)
	}
	if ir.cursorPos != 0 {
		t.Fatalf("expected cursorPos=0, got %d", ir.cursorPos)
	}
}

// ─── handleSearchByte ────────────────────────────────────────────────────────

func TestHandleSearchByte_Printable(t *testing.T) {
	ir := newSearchIR([]string{"hello world", "hello foo", "bar baz"})
	ir.enterSearchMode()

	// Type 'h'
	ir.handleSearchByte('h')
	if ir.searchQuery != "h" {
		t.Fatalf("expected searchQuery='h', got %q", ir.searchQuery)
	}
	// searchHistory searches backwards from end; "hello foo" (index 1) is the newest match.
	if ir.searchResult != "hello foo" {
		t.Fatalf("expected searchResult='hello foo', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 1 {
		t.Fatalf("expected searchResultIndex=1, got %d", ir.searchResultIndex)
	}

	// Type 'e'
	ir.handleSearchByte('e')
	if ir.searchQuery != "he" {
		t.Fatalf("expected searchQuery='he', got %q", ir.searchQuery)
	}
	if ir.searchResult != "hello foo" {
		t.Fatalf("expected searchResult='hello foo', got %q", ir.searchResult)
	}

	// Type 'l'
	ir.handleSearchByte('l')
	if ir.searchQuery != "hel" {
		t.Fatalf("expected searchQuery='hel', got %q", ir.searchQuery)
	}

	// Type 'l'
	ir.handleSearchByte('l')
	if ir.searchQuery != "hell" {
		t.Fatalf("expected searchQuery='hell', got %q", ir.searchQuery)
	}

	// Type 'o'
	ir.handleSearchByte('o')
	if ir.searchQuery != "hello" {
		t.Fatalf("expected searchQuery='hello', got %q", ir.searchQuery)
	}
	// "hello foo" (index 1) is still the newest match.
	if ir.searchResult != "hello foo" {
		t.Fatalf("expected searchResult='hello foo', got %q", ir.searchResult)
	}
}

func TestHandleSearchByte_Printable_NarrowingSearch(t *testing.T) {
	ir := newSearchIR([]string{"hello world", "hello foo", "bar"})
	ir.enterSearchMode()

	// Type "hello " (hello followed by space)
	for _, c := range "hello " {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchQuery != "hello " {
		t.Fatalf("expected searchQuery='hello ', got %q", ir.searchQuery)
	}
	// "hello " matches both "hello world" and "hello foo"; newest is "hello foo" (index 1)
	if ir.searchResult != "hello foo" {
		t.Fatalf("expected searchResult='hello foo', got %q", ir.searchResult)
	}

	// Type 'w' — should narrow to "hello world"
	ir.handleSearchByte('w')
	if ir.searchQuery != "hello w" {
		t.Fatalf("expected searchQuery='hello w', got %q", ir.searchQuery)
	}
	if ir.searchResult != "hello world" {
		t.Fatalf("expected searchResult='hello world', got %q", ir.searchResult)
	}

	// Type 'o' 'r' 'l' 'd'
	for _, c := range "orld" {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchQuery != "hello world" {
		t.Fatalf("expected searchQuery='hello world', got %q", ir.searchQuery)
	}
	if ir.searchResult != "hello world" {
		t.Fatalf("expected searchResult='hello world', got %q", ir.searchResult)
	}
}

func TestHandleSearchByte_Printable_NoMatch(t *testing.T) {
	ir := newSearchIR([]string{"aaa", "bbb"})
	ir.enterSearchMode()

	// Type 'x' — no matches
	ir.handleSearchByte('x')
	if ir.searchQuery != "x" {
		t.Fatalf("expected searchQuery='x', got %q", ir.searchQuery)
	}
	if ir.searchResult != "" {
		t.Fatalf("expected empty searchResult for no match, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != -1 {
		t.Fatalf("expected searchResultIndex=-1, got %d", ir.searchResultIndex)
	}
}

func TestHandleSearchByte_Backspace(t *testing.T) {
	ir := newSearchIR([]string{"hello world", "hello foo", "bar"})
	ir.enterSearchMode()

	// Build up search to "hello w"
	for _, c := range "hello w" {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchQuery != "hello w" {
		t.Fatalf("expected searchQuery='hello w', got %q", ir.searchQuery)
	}
	if ir.searchResult != "hello world" {
		t.Fatalf("expected 'hello world', got %q", ir.searchResult)
	}

	// Backspace 'w' → "hello " — should match "hello foo" (newest match with "hello ")
	ir.handleSearchByte(127) // Backspace (DEL)
	if ir.searchQuery != "hello " {
		t.Fatalf("expected searchQuery='hello ', got %q", ir.searchQuery)
	}
	if ir.searchResult != "hello foo" {
		t.Fatalf("expected 'hello foo' after backspace, got %q", ir.searchResult)
	}

	// Backspace ' ' → "hello" — should match "hello foo" (newest match with "hello")
	ir.handleSearchByte(8) // Backspace (BS)
	if ir.searchQuery != "hello" {
		t.Fatalf("expected searchQuery='hello', got %q", ir.searchQuery)
	}
	if ir.searchResult != "hello foo" {
		t.Fatalf("expected 'hello foo' after backspace, got %q", ir.searchResult)
	}
}

func TestHandleSearchByte_Backspace_ToEmpty(t *testing.T) {
	ir := newSearchIR([]string{"cmd1", "cmd2"})
	ir.enterSearchMode()

	// Type "cmd"
	for _, c := range "cmd" {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchQuery != "cmd" {
		t.Fatalf("expected searchQuery='cmd', got %q", ir.searchQuery)
	}

	// Backspace 3 times to clear query
	for i := 0; i < 3; i++ {
		ir.handleSearchByte(127)
	}
	if ir.searchQuery != "" {
		t.Fatalf("expected empty searchQuery after full backspace, got %q", ir.searchQuery)
	}

	// Empty query should show most recent history entry
	if ir.searchResult != "cmd2" {
		t.Fatalf("expected searchResult='cmd2' (most recent) for empty query, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 1 {
		t.Fatalf("expected searchResultIndex=1, got %d", ir.searchResultIndex)
	}
}

func TestHandleSearchByte_Backspace_EmptyQuery_NoHistory(t *testing.T) {
	ir := newSearchIR(nil)
	ir.enterSearchMode()

	// Should have empty result with no history
	if ir.searchResult != "" {
		t.Fatalf("expected empty searchResult with no history, got %q", ir.searchResult)
	}

	// Backspace on empty query with no history
	ir.handleSearchByte(127)
	if ir.searchResult != "" {
		t.Fatalf("expected empty searchResult after backspace with no history, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != -1 {
		t.Fatalf("expected searchResultIndex=-1, got %d", ir.searchResultIndex)
	}
}

func TestHandleSearchByte_Ignored(t *testing.T) {
	ir := newSearchIR([]string{"hello"})
	ir.enterSearchMode()

	// Type a character first
	ir.handleSearchByte('h')
	queryBefore := ir.searchQuery
	resultBefore := ir.searchResult

	// Send control character (byte 1) — should be ignored
	ir.handleSearchByte(1)
	if ir.searchQuery != queryBefore {
		t.Fatalf("expected searchQuery unchanged after control char, got %q (was %q)", ir.searchQuery, queryBefore)
	}
	if ir.searchResult != resultBefore {
		t.Fatalf("expected searchResult unchanged after control char, got %q (was %q)", ir.searchResult, resultBefore)
	}

	// Send control character (byte 2) — should be ignored
	ir.handleSearchByte(2)
	if ir.searchQuery != queryBefore {
		t.Fatalf("expected searchQuery unchanged after ctrl-B, got %q", ir.searchQuery)
	}

	// Send newline-like control char (byte 10, LF) — should be ignored
	ir.handleSearchByte(10)
	if ir.searchQuery != queryBefore {
		t.Fatalf("expected searchQuery unchanged after LF, got %q", ir.searchQuery)
	}

	// Tab (byte 9) — should be ignored
	ir.handleSearchByte(9)
	if ir.searchQuery != queryBefore {
		t.Fatalf("expected searchQuery unchanged after Tab, got %q", ir.searchQuery)
	}
}

// ─── cycleSearchResult ───────────────────────────────────────────────────────

func TestCycleSearchResult(t *testing.T) {
	ir := newSearchIR([]string{"echo hello", "echo world", "echo hello world"})
	ir.enterSearchMode()

	// Type "echo" to filter results
	for _, c := range "echo" {
		ir.handleSearchByte(byte(c))
	}
	// Most recent match: "echo hello world" at index 2
	if ir.searchResult != "echo hello world" {
		t.Fatalf("expected initial match 'echo hello world', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 2 {
		t.Fatalf("expected initial index 2, got %d", ir.searchResultIndex)
	}

	// First Ctrl-R cycle: should find "echo world" at index 1
	ir.cycleSearchResult()
	if ir.searchResult != "echo world" {
		t.Fatalf("expected cycled result 'echo world', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 1 {
		t.Fatalf("expected cycled index 1, got %d", ir.searchResultIndex)
	}

	// Second Ctrl-R cycle: should find "echo hello" at index 0
	ir.cycleSearchResult()
	if ir.searchResult != "echo hello" {
		t.Fatalf("expected cycled result 'echo hello', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 0 {
		t.Fatalf("expected cycled index 0, got %d", ir.searchResultIndex)
	}
}

func TestCycleSearchResult_EmptyQuery(t *testing.T) {
	ir := newSearchIR([]string{"aaa", "bbb", "ccc"})
	ir.enterSearchMode()

	// With empty query, initial result is most recent: "ccc" at index 2
	if ir.searchResult != "ccc" {
		t.Fatalf("expected initial result 'ccc', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 2 {
		t.Fatalf("expected initial index 2, got %d", ir.searchResultIndex)
	}

	// First cycle: should go to "bbb" at index 1
	ir.cycleSearchResult()
	if ir.searchResult != "bbb" {
		t.Fatalf("expected cycled result 'bbb', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 1 {
		t.Fatalf("expected cycled index 1, got %d", ir.searchResultIndex)
	}

	// Second cycle: should go to "aaa" at index 0
	ir.cycleSearchResult()
	if ir.searchResult != "aaa" {
		t.Fatalf("expected cycled result 'aaa', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 0 {
		t.Fatalf("expected cycled index 0, got %d", ir.searchResultIndex)
	}

	// Third cycle: should wrap around to "ccc" at index 2
	ir.cycleSearchResult()
	if ir.searchResult != "ccc" {
		t.Fatalf("expected wrapped result 'ccc', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 2 {
		t.Fatalf("expected wrapped index 2, got %d", ir.searchResultIndex)
	}
}

func TestCycleSearchResult_EmptyQuery_NoHistory(t *testing.T) {
	ir := newSearchIR(nil)
	ir.enterSearchMode()

	// No history: cycling should not crash or change state
	ir.cycleSearchResult()
	if ir.searchResult != "" {
		t.Fatalf("expected empty result, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != -1 {
		t.Fatalf("expected index -1, got %d", ir.searchResultIndex)
	}
}

func TestCycleSearchResult_NoMoreMatches(t *testing.T) {
	ir := newSearchIR([]string{"aaa match", "bbb"})
	ir.enterSearchMode()

	// Type "match" — only one match: "aaa match" at index 0
	for _, c := range "match" {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchResult != "aaa match" {
		t.Fatalf("expected match 'aaa match', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 0 {
		t.Fatalf("expected index 0, got %d", ir.searchResultIndex)
	}

	// Cycle again — no more matches, should keep current result
	ir.cycleSearchResult()
	if ir.searchResult != "aaa match" {
		t.Fatalf("expected result to stay 'aaa match' after cycling past last match, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 0 {
		t.Fatalf("expected index to stay 0, got %d", ir.searchResultIndex)
	}

	// Cycle again — still no change
	ir.cycleSearchResult()
	if ir.searchResult != "aaa match" {
		t.Fatalf("expected result to remain 'aaa match' after another cycle, got %q", ir.searchResult)
	}
}

func TestCycleSearchResult_SingleHistoryEntry(t *testing.T) {
	ir := newSearchIR([]string{"only entry"})
	ir.enterSearchMode()

	// Initial: "only entry" at index 0
	if ir.searchResult != "only entry" {
		t.Fatalf("expected 'only entry', got %q", ir.searchResult)
	}

	// Cycle with empty query: should wrap back to itself
	ir.cycleSearchResult()
	if ir.searchResult != "only entry" {
		t.Fatalf("expected 'only entry' after cycle, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 0 {
		t.Fatalf("expected index 0, got %d", ir.searchResultIndex)
	}
}

func TestCycleSearchResult_NoMatchKeepsCurrent(t *testing.T) {
	ir := newSearchIR([]string{"aaa", "bbb"})
	ir.enterSearchMode()

	// Type "nomatch" — no matches
	for _, c := range "nomatch" {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchResult != "" {
		t.Fatalf("expected empty result for no match, got %q", ir.searchResult)
	}

	// Cycle with no matches — should not crash, result stays empty
	ir.cycleSearchResult()
	if ir.searchResult != "" {
		t.Fatalf("expected empty result after cycle, got %q", ir.searchResult)
	}
}

// ─── renderSearchPrompt ──────────────────────────────────────────────────────

func TestRenderSearchPrompt_WithMatch(t *testing.T) {
	ir := newSearchIR([]string{"cmd1"})
	ir.enterSearchMode()
	ir.searchQuery = "cmd"
	ir.searchResult = "cmd1"

	output := captureStdoutSearch(func() {
		ir.renderSearchPrompt()
	})

	// Should contain the search format "(reverse-i-search)'query': result"
	if !strings.Contains(output, "(reverse-i-search)") {
		t.Fatalf("expected output to contain '(reverse-i-search)', got %q", output)
	}
	if !strings.Contains(output, "cmd") {
		t.Fatalf("expected output to contain query 'cmd', got %q", output)
	}
	if !strings.Contains(output, "cmd1") {
		t.Fatalf("expected output to contain result 'cmd1', got %q", output)
	}
}

func TestRenderSearchPrompt_NoMatch(t *testing.T) {
	ir := newSearchIR([]string{"aaa"})
	ir.enterSearchMode()
	ir.searchQuery = "nomatch"
	ir.searchResult = ""

	output := captureStdoutSearch(func() {
		ir.renderSearchPrompt()
	})

	// Should contain "(failing reverse-i-search)"
	if !strings.Contains(output, "(failing reverse-i-search)") {
		t.Fatalf("expected output to contain '(failing reverse-i-search)', got %q", output)
	}
	if !strings.Contains(output, "nomatch") {
		t.Fatalf("expected output to contain query 'nomatch', got %q", output)
	}
}

func TestRenderSearchPrompt_EmptyQuery(t *testing.T) {
	ir := newSearchIR([]string{"cmd1"})
	ir.enterSearchMode()
	// searchQuery is empty, searchResult is "cmd1"

	output := captureStdoutSearch(func() {
		ir.renderSearchPrompt()
	})

	// Should show (reverse-i-search) with empty query
	if !strings.Contains(output, "(reverse-i-search)") {
		t.Fatalf("expected output to contain '(reverse-i-search)', got %q", output)
	}
}

func TestRenderSearchPrompt_ClearsLine(t *testing.T) {
	ir := newSearchIR(nil)
	ir.searchQuery = "test"

	output := captureStdoutSearch(func() {
		ir.renderSearchPrompt()
	})

	// Should start with carriage return and clear line sequence
	if !strings.HasPrefix(output, "\r") {
		t.Fatalf("expected output to start with '\\r', got %q", output)
	}
	if !strings.Contains(output, ClearLineSeq()) {
		t.Fatalf("expected output to contain clear line sequence, got %q", output)
	}
}

// ─── Integration Tests ───────────────────────────────────────────────────────

func TestSearchHistoryIntegration_AcceptMatch(t *testing.T) {
	ir := newSearchIR([]string{"ls -la", "echo hello", "ls -lh"})
	ir.line = "current line"
	ir.cursorPos = 5

	// Enter search mode
	ir.enterSearchMode()

	// Type "ls -l"
	for _, c := range "ls -l" {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchQuery != "ls -l" {
		t.Fatalf("expected query 'ls -l', got %q", ir.searchQuery)
	}
	// Most recent match: "ls -lh" at index 2
	if ir.searchResult != "ls -lh" {
		t.Fatalf("expected result 'ls -lh', got %q", ir.searchResult)
	}

	// Accept the match
	ir.exitSearchMode(true)

	if ir.line != "ls -lh" {
		t.Fatalf("expected line='ls -lh' after accept, got %q", ir.line)
	}
	if ir.cursorPos != 6 {
		t.Fatalf("expected cursorPos=6 (len of 'ls -lh'), got %d", ir.cursorPos)
	}
	if ir.searchMode {
		t.Fatal("expected searchMode=false after accept")
	}
}

func TestSearchHistoryIntegration_Cancel(t *testing.T) {
	ir := newSearchIR([]string{"cmd1", "cmd2"})
	ir.line = "original line"
	ir.cursorPos = 7

	// Enter search mode
	ir.enterSearchMode()

	// Type some query
	for _, c := range "some" {
		ir.handleSearchByte(byte(c))
	}

	// Cancel
	ir.exitSearchMode(false)

	// Should restore original state
	if ir.line != "original line" {
		t.Fatalf("expected line='original line' after cancel, got %q", ir.line)
	}
	if ir.cursorPos != 7 {
		t.Fatalf("expected cursorPos=7 after cancel, got %d", ir.cursorPos)
	}
	if ir.searchMode {
		t.Fatal("expected searchMode=false after cancel")
	}
}

func TestSearchHistoryIntegration_CycleThenAccept(t *testing.T) {
	ir := newSearchIR([]string{"echo hello", "echo world", "echo foo"})
	ir.line = ""
	ir.cursorPos = 0

	// Enter search mode
	ir.enterSearchMode()

	// Type "echo"
	for _, c := range "echo" {
		ir.handleSearchByte(byte(c))
	}
	// Initial: most recent match "echo foo" at index 2
	if ir.searchResult != "echo foo" {
		t.Fatalf("expected initial match 'echo foo', got %q", ir.searchResult)
	}

	// Cycle to find "echo world"
	ir.cycleSearchResult()
	if ir.searchResult != "echo world" {
		t.Fatalf("expected cycled to 'echo world', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 1 {
		t.Fatalf("expected index 1, got %d", ir.searchResultIndex)
	}

	// Accept the cycled result
	ir.exitSearchMode(true)

	if ir.line != "echo world" {
		t.Fatalf("expected line='echo world' after accept, got %q", ir.line)
	}
	if ir.cursorPos != 10 {
		t.Fatalf("expected cursorPos=10 (len of 'echo world'), got %d", ir.cursorPos)
	}
}

func TestSearchHistoryIntegration_CaseInsensitiveAccept(t *testing.T) {
	ir := newSearchIR([]string{"Hello World", "Goodbye"})
	ir.line = ""
	ir.cursorPos = 0

	// Enter search mode
	ir.enterSearchMode()

	// Type "HELLO" (uppercase)
	for _, c := range "HELLO" {
		ir.handleSearchByte(byte(c))
	}

	// Should match "Hello World" case-insensitively
	if ir.searchResult != "Hello World" {
		t.Fatalf("expected case-insensitive match 'Hello World', got %q", ir.searchResult)
	}

	// Accept
	ir.exitSearchMode(true)

	if ir.line != "Hello World" {
		t.Fatalf("expected line='Hello World', got %q", ir.line)
	}
}

func TestSearchHistoryIntegration_BackspaceToEmptyThenCancel(t *testing.T) {
	ir := newSearchIR([]string{"cmd1"})
	ir.line = "pre line"
	ir.cursorPos = 3

	ir.enterSearchMode()

	// Type "abc"
	for _, c := range "abc" {
		ir.handleSearchByte(byte(c))
	}
	// Backspace all the way to empty
	for i := 0; i < 3; i++ {
		ir.handleSearchByte(127)
	}

	// Should show most recent entry with empty query
	if ir.searchQuery != "" {
		t.Fatalf("expected empty query, got %q", ir.searchQuery)
	}
	if ir.searchResult != "cmd1" {
		t.Fatalf("expected most recent 'cmd1' for empty query, got %q", ir.searchResult)
	}

	// Cancel — should restore pre-search state
	ir.exitSearchMode(false)
	if ir.line != "pre line" {
		t.Fatalf("expected line='pre line', got %q", ir.line)
	}
	if ir.cursorPos != 3 {
		t.Fatalf("expected cursorPos=3, got %d", ir.cursorPos)
	}
}

func TestSearchHistoryIntegration_NoMatchesThenCancel(t *testing.T) {
	ir := newSearchIR([]string{"cmd1", "cmd2"})
	ir.line = "existing"
	ir.cursorPos = 8

	ir.enterSearchMode()

	// Type "nonexistent"
	for _, c := range "nonexistent" {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchResult != "" {
		t.Fatalf("expected no match, got %q", ir.searchResult)
	}

	// Accept with no match — line should be set to empty searchResult (which is empty)
	// Actually with accept=true and empty searchResult, the condition is:
	// if accept && ir.searchResult != "" { ... }
	// So with empty result, it falls into the else branch and restores pre-search state.
	ir.exitSearchMode(true)

	// With empty searchResult and accept=true, the code checks `ir.searchResult != ""`
	// which is false, so it falls through to the else branch (restore).
	if ir.line != "existing" {
		t.Fatalf("expected line restored to 'existing' (empty result + accept falls through to restore), got %q", ir.line)
	}
	if ir.cursorPos != 8 {
		t.Fatalf("expected cursorPos=8, got %d", ir.cursorPos)
	}
}

func TestSearchHistoryIntegration_MultipleCycles_NoMore(t *testing.T) {
	ir := newSearchIR([]string{"one match", "another", "no match here"})
	ir.line = "current"
	ir.cursorPos = 7

	ir.enterSearchMode()

	// Type "one"
	for _, c := range "one" {
		ir.handleSearchByte(byte(c))
	}
	// Should find "one match" at index 0
	if ir.searchResult != "one match" {
		t.Fatalf("expected 'one match', got %q", ir.searchResult)
	}

	// Cycle — no more matches
	ir.cycleSearchResult()
	if ir.searchResult != "one match" {
		t.Fatalf("expected result to stay 'one match', got %q", ir.searchResult)
	}

	// Cycle again — still no change
	ir.cycleSearchResult()
	if ir.searchResult != "one match" {
		t.Fatalf("expected result to stay 'one match' after 2nd extra cycle, got %q", ir.searchResult)
	}

	// Accept
	ir.exitSearchMode(true)
	if ir.line != "one match" {
		t.Fatalf("expected line='one match', got %q", ir.line)
	}
}

// ─── Edge Cases ──────────────────────────────────────────────────────────────

func TestSearchHistory_MultiByteUTF8(t *testing.T) {
	ir := newSearchIR([]string{"café", "日本語", "hello"})

	// Search for "café"
	result, _, ok := ir.searchHistory("café", len(ir.history)-1)
	if !ok {
		t.Fatal("expected match for 'café'")
	}
	if result != "café" {
		t.Fatalf("expected 'café', got %q", result)
	}

	// Search for "日本"
	result, _, ok = ir.searchHistory("日本", len(ir.history)-1)
	if !ok {
		t.Fatal("expected match for '日本'")
	}
	if result != "日本語" {
		t.Fatalf("expected '日本語', got %q", result)
	}
}

func TestSearchHistory_SubstringAtVariousPositions(t *testing.T) {
	ir := newSearchIR([]string{"prefix_MATCH_suffix", "MATCH_only", "only_MATCH"})

	// "MATCH" should match all three (case-sensitive)
	_, idx, ok := ir.searchHistory("MATCH", len(ir.history)-1)
	if !ok || idx != 2 {
		t.Fatalf("expected index 2 ('only_MATCH'), got ok=%v idx=%d", ok, idx)
	}

	// "prefix" should match "prefix_MATCH_suffix"
	_, idx, ok = ir.searchHistory("prefix", len(ir.history)-1)
	if !ok || idx != 0 {
		t.Fatalf("expected index 0, got ok=%v idx=%d", ok, idx)
	}

	// "suffix" should match "prefix_MATCH_suffix"
	_, idx, ok = ir.searchHistory("suffix", len(ir.history)-1)
	if !ok || idx != 0 {
		t.Fatalf("expected index 0, got ok=%v idx=%d", ok, idx)
	}
}

func TestHandleSearchByte_MixedCaseQuery(t *testing.T) {
	ir := newSearchIR([]string{"Hello World", "hello world", "HELLO"})

	ir.enterSearchMode()

	// Type "hELLO" (mixed case)
	for _, c := range "hELLO" {
		ir.handleSearchByte(byte(c))
	}
	if ir.searchQuery != "hELLO" {
		t.Fatalf("expected searchQuery='hELLO', got %q", ir.searchQuery)
	}

	// Should find the newest entry that contains "hELLO" case-insensitively
	// "HELLO" at index 2 contains "HELLO" which matches "hELLO" case-insensitively
	// "hello world" at index 1 contains "hello" which matches "hELLO" case-insensitively
	// "Hello World" at index 0 contains "Hello" which matches "hELLO" case-insensitively
	// Most recent match should be "HELLO" at index 2
	if ir.searchResult != "HELLO" {
		t.Fatalf("expected searchResult='HELLO', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 2 {
		t.Fatalf("expected searchResultIndex=2, got %d", ir.searchResultIndex)
	}
}

func TestEnterSearchMode_WithSingleHistoryEntry(t *testing.T) {
	ir := newSearchIR([]string{"only command"})

	ir.enterSearchMode()

	if ir.searchResult != "only command" {
		t.Fatalf("expected 'only command', got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 0 {
		t.Fatalf("expected index 0, got %d", ir.searchResultIndex)
	}
}

func TestSearchHistory_NegativeStartIndex_Normalized(t *testing.T) {
	ir := newSearchIR([]string{"aaa", "bbb", "ccc"})

	// startIndex=-1 should normalize to len(history)-1 = 2
	_, idx, ok := ir.searchHistory("aaa", -1)
	if !ok {
		t.Fatal("expected match when startIndex is normalized")
	}
	if idx != 0 {
		t.Fatalf("expected index 0, got %d", idx)
	}

	// startIndex=-999 should also normalize to len(history)-1 = 2
	_, idx, ok = ir.searchHistory("bbb", -999)
	if !ok {
		t.Fatal("expected match with very negative startIndex")
	}
	if idx != 1 {
		t.Fatalf("expected index 1, got %d", idx)
	}
}

func TestCycleSearchResult_EmptyQuery_SingleEntry(t *testing.T) {
	ir := newSearchIR([]string{"only"})
	ir.enterSearchMode()

	// Already on "only" at index 0
	ir.cycleSearchResult()
	// Should wrap to index 0 (only entry)
	if ir.searchResult != "only" {
		t.Fatalf("expected 'only' after cycle, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 0 {
		t.Fatalf("expected index 0, got %d", ir.searchResultIndex)
	}
}

func TestSearchHistoryIntegration_AcceptOlderMatchAfterCycle(t *testing.T) {
	ir := newSearchIR([]string{
		"git commit -m 'initial'",
		"git push origin main",
		"git commit -m 'fix bug'",
	})
	ir.line = ""
	ir.cursorPos = 0

	ir.enterSearchMode()

	// Type "git commit"
	for _, c := range "git commit" {
		ir.handleSearchByte(byte(c))
	}
	// Most recent: "git commit -m 'fix bug'" at index 2
	if ir.searchResult != "git commit -m 'fix bug'" {
		t.Fatalf("expected initial match, got %q", ir.searchResult)
	}

	// Cycle to get older match
	ir.cycleSearchResult()
	if ir.searchResult != "git commit -m 'initial'" {
		t.Fatalf("expected cycled to older match, got %q", ir.searchResult)
	}
	if ir.searchResultIndex != 0 {
		t.Fatalf("expected index 0, got %d", ir.searchResultIndex)
	}

	// Accept the older match
	ir.exitSearchMode(true)
	if ir.line != "git commit -m 'initial'" {
		t.Fatalf("expected line to be older match, got %q", ir.line)
	}
}

func TestHandleSearchByte_PreservesQueryAfterNoMatch(t *testing.T) {
	ir := newSearchIR([]string{"aaa"})
	ir.enterSearchMode()

	// Type "xyz" — no match
	for _, c := range "xyz" {
		ir.handleSearchByte(byte(c))
	}

	if ir.searchQuery != "xyz" {
		t.Fatalf("expected query preserved as 'xyz', got %q", ir.searchQuery)
	}
	if ir.searchResult != "" {
		t.Fatalf("expected no match, got %q", ir.searchResult)
	}

	// Backspace 'z' → "xy" — still no match
	ir.handleSearchByte(127)
	if ir.searchQuery != "xy" {
		t.Fatalf("expected query 'xy', got %q", ir.searchQuery)
	}
	if ir.searchResult != "" {
		t.Fatalf("expected no match for 'xy', got %q", ir.searchResult)
	}

	// Backspace 'y' → "x" — still no match
	ir.handleSearchByte(127)
	if ir.searchQuery != "x" {
		t.Fatalf("expected query 'x', got %q", ir.searchQuery)
	}

	// Backspace 'x' → "" — empty query, should show most recent
	ir.handleSearchByte(127)
	if ir.searchQuery != "" {
		t.Fatalf("expected empty query, got %q", ir.searchQuery)
	}
	if ir.searchResult != "aaa" {
		t.Fatalf("expected 'aaa' for empty query, got %q", ir.searchResult)
	}
}

func TestSearchHistory_EmptyQuery_MatchesFirst(t *testing.T) {
	ir := newSearchIR([]string{"first", "second", "third"})

	// Empty query should match everything, returning the first entry scanned
	result, idx, ok := ir.searchHistory("", 2)
	if !ok {
		t.Fatal("expected empty query to match")
	}
	if idx != 2 {
		t.Fatalf("expected index 2 for empty query starting at 2, got %d", idx)
	}
	if result != "third" {
		t.Fatalf("expected 'third', got %q", result)
	}
}

func TestCycleSearchResult_PartialMatchWrap(t *testing.T) {
	ir := newSearchIR([]string{"foo", "foo bar", "baz"})
	ir.enterSearchMode()

	// Type "foo"
	for _, c := range "foo" {
		ir.handleSearchByte(byte(c))
	}
	// Should find "foo bar" at index 1 (most recent containing "foo")
	if ir.searchResult != "foo bar" {
		t.Fatalf("expected 'foo bar', got %q", ir.searchResult)
	}

	// Cycle → should find "foo" at index 0
	ir.cycleSearchResult()
	if ir.searchResult != "foo" {
		t.Fatalf("expected 'foo', got %q", ir.searchResult)
	}

	// Cycle again → wraps around, searches from end; finds "foo bar" at index 1 again
	ir.cycleSearchResult()
	if ir.searchResult != "foo bar" {
		t.Fatalf("expected 'foo bar' (wrap-around), got %q", ir.searchResult)
	}
}
