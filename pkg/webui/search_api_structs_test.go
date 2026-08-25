//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// SearchResponse / SearchResult / SearchMatch — JSON round-trip
// ---------------------------------------------------------------------------

func TestSearchResponse_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	resp := SearchResponse{
		Results: []SearchResult{
			{
				File:       "pkg/main.go",
				MatchCount: 2,
				Matches: []SearchMatch{
					{
						LineNumber:    10,
						Line:          "func main() {}",
						ColumnStart:   1,
						ColumnEnd:     4,
						ContextBefore: []string{"package main", "import \"fmt\""},
						ContextAfter:  []string{"// end"},
					},
				},
			},
		},
		TotalMatches: 2,
		TotalFiles:   1,
		Truncated:    false,
		Query:        "main",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got SearchResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Query != "main" {
		t.Errorf("query = %q; want 'main'", got.Query)
	}
	if got.TotalMatches != 2 {
		t.Errorf("total_matches = %d; want 2", got.TotalMatches)
	}
	if got.TotalFiles != 1 {
		t.Errorf("total_files = %d; want 1", got.TotalFiles)
	}
	if got.Truncated {
		t.Error("truncated should be false")
	}
	if len(got.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got.Results))
	}
	if got.Results[0].File != "pkg/main.go" {
		t.Errorf("file = %q; want 'pkg/main.go'", got.Results[0].File)
	}
	if got.Results[0].MatchCount != 2 {
		t.Errorf("match_count = %d; want 2", got.Results[0].MatchCount)
	}
	m := got.Results[0].Matches[0]
	if m.LineNumber != 10 || m.ColumnStart != 1 || m.ColumnEnd != 4 {
		t.Errorf("match fields wrong: %+v", m)
	}
	if len(m.ContextBefore) != 2 || len(m.ContextAfter) != 1 {
		t.Errorf("context wrong: before=%d, after=%d", len(m.ContextBefore), len(m.ContextAfter))
	}
}

func TestSearchResponse_EmptyResultsJSON(t *testing.T) {
	t.Parallel()
	resp := SearchResponse{
		Results:      nil,
		TotalMatches: 0,
		TotalFiles:   0,
		Truncated:    false,
		Query:        "nothing",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got SearchResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Query != "nothing" {
		t.Errorf("query = %q; want 'nothing'", got.Query)
	}
}

func TestSearchResponse_TruncatedJSON(t *testing.T) {
	t.Parallel()
	resp := SearchResponse{
		Results:      []SearchResult{},
		TotalMatches: 5001,
		TotalFiles:   100,
		Truncated:    true,
		Query:        "big",
	}

	data, _ := json.Marshal(resp)
	var got SearchResponse
	json.Unmarshal(data, &got)

	if !got.Truncated {
		t.Error("truncated should be true after round-trip")
	}
}

func TestSearchMatch_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	m := SearchMatch{
		LineNumber:    42,
		Line:          "func foo() {}",
		ColumnStart:   1,
		ColumnEnd:     4,
		ContextBefore: []string{"package main"},
		ContextAfter:  []string{},
	}

	data, _ := json.Marshal(m)
	var got SearchMatch
	json.Unmarshal(data, &got)

	if got.LineNumber != 42 || got.ColumnStart != 1 || got.ColumnEnd != 4 {
		t.Errorf("fields wrong: %+v", got)
	}
}

func TestSearchResult_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	r := SearchResult{
		File:       "test.go",
		MatchCount: 5,
		Matches:    []SearchMatch{},
	}

	data, _ := json.Marshal(r)
	var got SearchResult
	json.Unmarshal(data, &got)

	if got.File != "test.go" || got.MatchCount != 5 {
		t.Errorf("fields wrong: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// compileSearchPattern — additional edge cases
// ---------------------------------------------------------------------------

func TestCompileSearchPattern_RegexNonCapturingGroup(t *testing.T) {
	t.Parallel()
	pat, err := compileSearchPattern("(?:a|b)c", true, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pat.MatchString("ac") {
		t.Error("should match 'ac'")
	}
	if !pat.MatchString("bc") {
		t.Error("should match 'bc'")
	}
	if pat.MatchString("dc") {
		t.Error("should not match 'dc'")
	}
}

func TestCompileSearchPattern_Backslash(t *testing.T) {
	t.Parallel()
	pat, err := compileSearchPattern("\\n", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pat.MatchString(`\n`) {
		t.Error("should match literal backslash-n")
	}
	if pat.MatchString("n") {
		t.Error("should not match just 'n' without the backslash")
	}
}

func TestCompileSearchPattern_StarsAndPlus(t *testing.T) {
	t.Parallel()
	pat, err := compileSearchPattern("*+?{}|", false, false, false)
	if err != nil {
		t.Fatalf("plain text with regex chars should not error: %v", err)
	}
	if !pat.MatchString("*+?{}|") {
		t.Error("should match literal special chars")
	}
}

func TestCompileSearchPattern_CaretDollarEscaped(t *testing.T) {
	t.Parallel()
	pat, err := compileSearchPattern("^test$", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pat.MatchString("^test$") {
		t.Error("should match literal '^test$'")
	}
	if pat.MatchString("test") {
		t.Error("should not match 'test' (caret and dollar are escaped)")
	}
}

// ---------------------------------------------------------------------------
// compileSearchPattern — all flag combinations
// ---------------------------------------------------------------------------

func TestCompileSearchPattern_AllFlagCombinations(t *testing.T) {
	t.Parallel()
	queries := []string{"test", "[a-z]+"}
	for _, q := range queries {
		for i := 0; i < 8; i++ {
			cs := (i & 1) == 1
			ww := (i & 2) == 2
			rx := (i & 4) == 4
			label := fmt.Sprintf("%s/cs=%v/ww=%v/rx=%v", q, cs, ww, rx)
			t.Run(label, func(t *testing.T) {
				t.Parallel()
				pat, err := compileSearchPattern(q, cs, ww, rx)
				// If regex mode with a valid regex, should not error
				if rx && q == "[a-z]+" && err != nil {
					t.Fatalf("expected no error for valid regex: %v", err)
				}
				// Plain text should never return nil pattern
				if !rx && (pat == nil && err == nil) {
					t.Fatal("plain text should produce non-nil pattern with no error")
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// compileSearchPattern — invalid regex edge cases
// ---------------------------------------------------------------------------

func TestCompileSearchPattern_InvalidRegex_UnclosedGroup(t *testing.T) {
	t.Parallel()
	_, err := compileSearchPattern("(unclosed", false, false, true)
	if err == nil {
		t.Fatal("expected error for unclosed group")
	}
}

func TestCompileSearchPattern_InvalidRegex_BadCharClass(t *testing.T) {
	t.Parallel()
	_, err := compileSearchPattern("[a-", false, false, true)
	if err == nil {
		t.Fatal("expected error for bad character class")
	}
}

func TestCompileSearchPattern_PlainTextNoRegexError(t *testing.T) {
	t.Parallel()
	_, err := compileSearchPattern("[invalid", false, false, false)
	if err != nil {
		t.Fatalf("plain text should not error: %v", err)
	}
	_, err = compileSearchPattern("(unclosed", false, false, false)
	if err != nil {
		t.Fatalf("plain text should not error: %v", err)
	}
	_, err = compileSearchPattern("*star", false, false, false)
	if err != nil {
		t.Fatalf("plain text should not error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parsePatterns — edge cases
// ---------------------------------------------------------------------------

func TestParsePatterns_TrailingComma(t *testing.T) {
	t.Parallel()
	got := parsePatterns("*.go,*.ts,")
	if len(got) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(got))
	}
}

func TestParsePatterns_SkipsEmptyParts(t *testing.T) {
	t.Parallel()
	got := parsePatterns("*.go,,*.ts,  ,*.js")
	if len(got) != 3 {
		t.Fatalf("expected 3 patterns, got %d: %v", len(got), got)
	}
}

func TestParsePatterns_WithSpaces(t *testing.T) {
	t.Parallel()
	got := parsePatterns("*.go , *.ts , *.js")
	if len(got) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(got))
	}
	if got[0] != "*.go" || got[1] != "*.ts" || got[2] != "*.js" {
		t.Errorf("got %v; want trimmed values", got)
	}
}

// ---------------------------------------------------------------------------
// getContextLines — edge cases
// ---------------------------------------------------------------------------

func TestGetContextLines_ReadOnly(t *testing.T) {
	t.Parallel()
	buf := []string{"a", "b", "c", "d"}
	result := getContextLines(buf, 4, 2, true)
	if len(result) != 2 {
		t.Errorf("expected 2 lines, got %d", len(result))
	}
	// Verify original buffer was not modified
	if len(buf) != 4 || buf[0] != "a" {
		t.Error("getContextLines should not modify the input buffer")
	}
}

// ---------------------------------------------------------------------------
// isBinaryFile — edge cases
// ---------------------------------------------------------------------------

func TestIsBinaryFile_NonExistentPath(t *testing.T) {
	t.Parallel()
	result := isBinaryFile("/nonexistent/path/xyz")
	if result {
		t.Error("non-existent file should not be classified as binary")
	}
}

// ---------------------------------------------------------------------------
// matchesAnyPattern — edge cases
// ---------------------------------------------------------------------------

func TestMatchesAnyPattern_EmptyPatterns(t *testing.T) {
	t.Parallel()
	result := matchesAnyPattern("/any/path.go", []string{})
	if result {
		t.Error("should not match when patterns list is empty")
	}
}

func TestMatchesAnyPattern_NilPatterns(t *testing.T) {
	t.Parallel()
	result := matchesAnyPattern("/any/path.go", nil)
	if result {
		t.Error("should not match when patterns list is nil")
	}
}

func TestMatchesAnyPattern_CaseSensitive2(t *testing.T) {
	t.Parallel()
	result := matchesAnyPattern("/path/File.GOO", []string{"*.go"})
	if result {
		t.Error("filepath.Match is case-sensitive; *.go should not match File.GOO")
	}
}
