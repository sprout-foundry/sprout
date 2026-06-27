package search

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeNow() time.Time {
	return time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
}

func makeYesterday() time.Time {
	return time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
}

func makeOlder() time.Time {
	return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
}

func buildTestIndex(t *testing.T) *SessionIndex {
	t.Helper()
	return &SessionIndex{
		Version: 1,
		Sessions: map[string]SessionIndexEntry{
			"session-1": {
				SessionID:   "session-1",
				Name:        "Test Session One",
				WorkingDir:  "/home/user/project",
				LastUpdated: makeNow(),
				TotalCost:   0.05,
				MessageCount: 5,
				Tokens:      nil,
				Text:        "the embedding index was broken after the schema migration and needed fixing",
			},
			"session-2": {
				SessionID:   "session-2",
				Name:        "Auth Debugging",
				WorkingDir:  "/home/user/project",
				LastUpdated: makeYesterday(),
				TotalCost:   0.03,
				MessageCount: 3,
				Tokens:      nil,
				Text:        "openai auth was returning 401 errors on the api key validation",
			},
			"session-3": {
				SessionID:   "session-3",
				Name:        "Database Migration",
				WorkingDir:  "/home/user/other",
				LastUpdated: makeOlder(),
				TotalCost:   0.10,
				MessageCount: 8,
				Tokens:      nil,
				Text:        "the database migration script failed on the embedding index creation step",
			},
			"session-4": {
				SessionID:   "session-4",
				Name:        "Unrelated Session",
				WorkingDir:  "/tmp/scratch",
				LastUpdated: makeNow(),
				TotalCost:   0.01,
				MessageCount: 2,
				Tokens:      nil,
				Text:        "just testing some basic functionality here",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Test Search — ranking
// ---------------------------------------------------------------------------

func TestSearch_ExactPhrase(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: "embedding index"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// session-1 and session-3 both contain "embedding index" (tier 3).
	// session-2 has neither term, so it's excluded.
	if results[0].MatchScore != 3 {
		t.Errorf("first result should be tier 3 (exact phrase), got %d", results[0].MatchScore)
	}

	// Most recent should come first: session-1 (now) > session-3 (older).
	if results[0].SessionID != "session-1" {
		t.Errorf("expected session-1 first (more recent), got %s", results[0].SessionID)
	}
	if results[1].SessionID != "session-3" {
		t.Errorf("expected session-3 second, got %s", results[1].SessionID)
	}
}

func TestSearch_AllTerms(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: "auth 401"})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].SessionID != "session-2" {
		t.Errorf("expected session-2, got %s", results[0].SessionID)
	}

	if results[0].MatchScore != 2 {
		t.Errorf("expected tier 2 (all terms), got %d", results[0].MatchScore)
	}
}

func TestSearch_AnyTerm(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: "embedding migration"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// session-1 has both "embedding" and "migration" → tier 2 (more recent)
	// session-3 has both "embedding" and "migration" → tier 2 (older)
	// session-2 has neither → excluded

	if results[0].MatchScore != 2 {
		t.Errorf("first result should be tier 2, got %d", results[0].MatchScore)
	}
	if results[0].SessionID != "session-1" {
		t.Errorf("expected session-1 first (more recent at same tier), got %s with score %d", results[0].SessionID, results[0].MatchScore)
	}
	if results[1].SessionID != "session-3" {
		t.Errorf("expected session-3 second, got %s", results[1].SessionID)
	}
}

func TestSearch_NoResults(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: "xyzzy_nonexistent"})

	if results != nil && len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: ""})

	if results != nil && len(results) != 0 {
		t.Errorf("expected empty results for empty query, got %d", len(results))
	}
}

func TestSearch_NilIndex(t *testing.T) {
	results := Search(nil, SearchOptions{Query: "test"})
	if results != nil {
		t.Errorf("expected nil for nil index, got %v", results)
	}
}

func TestSearch_NilSessions(t *testing.T) {
	idx := &SessionIndex{Sessions: nil}
	results := Search(idx, SearchOptions{Query: "test"})
	if results != nil {
		t.Errorf("expected nil for nil sessions, got %v", results)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: "EMBEDDING INDEX"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results for uppercase query, got %d", len(results))
	}
}

func TestSearch_PhraseVsTerms(t *testing.T) {
	// Create entries where one has the terms but not the phrase, and one has the phrase.
	idx := &SessionIndex{
		Sessions: map[string]SessionIndexEntry{
			"a": {
				SessionID:   "a",
				Name:        "Has Phrase",
				WorkingDir:  "/home/user",
				LastUpdated: makeYesterday(),
				Text:        "the quick brown fox jumps over the lazy dog",
			},
			"b": {
				SessionID:   "b",
				Name:        "Has Terms",
				WorkingDir:  "/home/user",
				LastUpdated: makeNow(),
				Text:        "the quick response was good and the fox was fast",
			},
		},
	}

	results := Search(idx, SearchOptions{Query: "quick brown fox"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// "a" has the phrase → tier 3
	// "b" has "quick" and "fox" but not "brown" → tier 1
	if results[0].SessionID != "a" {
		t.Errorf("expected 'a' first (tier 3), got %s", results[0].SessionID)
	}
	if results[0].MatchScore != 3 {
		t.Errorf("expected tier 3, got %d", results[0].MatchScore)
	}
	if results[1].MatchScore != 1 {
		t.Errorf("expected tier 1 for 'b', got %d", results[1].MatchScore)
	}
}

// ---------------------------------------------------------------------------
// Test Search — filtering
// ---------------------------------------------------------------------------

func TestSearch_FilterWorkingDir(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{
		Query:      "embedding",
		WorkingDir: "/home/user/project",
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SessionID != "session-1" {
		t.Errorf("expected session-1, got %s", results[0].SessionID)
	}
}

func TestSearch_FilterSince(t *testing.T) {
	idx := buildTestIndex(t)
	cutoff := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	results := Search(idx, SearchOptions{
		Query: "embedding",
		Since: cutoff,
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 result with Since filter, got %d", len(results))
	}
	if results[0].SessionID != "session-1" {
		t.Errorf("expected session-1 (most recent), got %s", results[0].SessionID)
	}
}

func TestSearch_FilterUntil(t *testing.T) {
	idx := buildTestIndex(t)
	deadline := time.Date(2026, 6, 26, 23, 59, 59, 0, time.UTC)
	results := Search(idx, SearchOptions{
		Query: "embedding",
		Until: deadline,
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 result with Until filter, got %d", len(results))
	}
	// session-1 is now (2026-06-27) which is after deadline (2026-06-26), so excluded.
	// session-3 (2026-06-20) has "embedding" and is before deadline.
	if results[0].SessionID != "session-3" {
		t.Errorf("expected session-3, got %s", results[0].SessionID)
	}
}

func TestSearch_FilterCombined(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{
		Query:      "auth",
		WorkingDir: "/home/user/project",
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SessionID != "session-2" {
		t.Errorf("expected session-2, got %s", results[0].SessionID)
	}
}

func TestSearch_Limit(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{
		Query: "embedding",
		Limit: 1,
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 result with limit=1, got %d", len(results))
	}
	if results[0].SessionID != "session-1" {
		t.Errorf("expected session-1 (highest score), got %s", results[0].SessionID)
	}
}

func TestSearch_LimitZero(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{
		Query: "embedding",
		Limit: 0,
	})

	// Should default to 20, so all matching results returned
	if len(results) != 2 {
		t.Fatalf("expected 2 results with default limit, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Test Search — tie-breaking by recency
// ---------------------------------------------------------------------------

func TestSearch_TieBreakRecency(t *testing.T) {
	idx := &SessionIndex{
		Sessions: map[string]SessionIndexEntry{
			"old": {
				SessionID:   "old",
				Name:        "Old Session",
				WorkingDir:  "/home/user",
				LastUpdated: makeOlder(),
				Text:        "the embedding index needs fixing",
			},
			"new": {
				SessionID:   "new",
				Name:        "New Session",
				WorkingDir:  "/home/user",
				LastUpdated: makeNow(),
				Text:        "the embedding index was fixed yesterday",
			},
		},
	}

	results := Search(idx, SearchOptions{Query: "embedding index"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].SessionID != "new" {
		t.Errorf("expected 'new' first (more recent), got %s", results[0].SessionID)
	}
	if results[1].SessionID != "old" {
		t.Errorf("expected 'old' second, got %s", results[1].SessionID)
	}
}

// ---------------------------------------------------------------------------
// Test buildExcerpt
// ---------------------------------------------------------------------------

func TestBuildExcerpt_Basic(t *testing.T) {
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      "hello world this is a test message with some content to search through",
	}

	excerpt := buildExcerpt(entry, []string{"test", "message"}, "test message")

	if !strings.Contains(excerpt, "[test]") {
		t.Errorf("excerpt should contain [test], got: %s", excerpt)
	}
	if !strings.Contains(excerpt, "[message]") {
		t.Errorf("excerpt should contain [message], got: %s", excerpt)
	}
}

func TestBuildExcerpt_EllipsisStart(t *testing.T) {
	// Long text where the match is far into the string, so start gets trimmed.
	longText := strings.Repeat("padding text. ", 20) + "the target phrase we are looking for"
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      longText,
	}

	excerpt := buildExcerpt(entry, []string{"target", "phrase"}, "target phrase")

	if !strings.HasPrefix(excerpt, "...") {
		t.Errorf("excerpt should start with ..., got: %s", excerpt)
	}
	if !strings.Contains(excerpt, "[target]") {
		t.Errorf("excerpt should bracket 'target', got: %s", excerpt)
	}
	if !strings.Contains(excerpt, "[phrase]") {
		t.Errorf("excerpt should bracket 'phrase', got: %s", excerpt)
	}
}

func TestBuildExcerpt_EllipsisEnd(t *testing.T) {
	// Match near start but text extends far beyond +120.
	longText := "the target phrase at the start " + strings.Repeat("more text here. ", 30)
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      longText,
	}

	excerpt := buildExcerpt(entry, []string{"target", "phrase"}, "target phrase")

	if !strings.HasSuffix(excerpt, "...") {
		t.Errorf("excerpt should end with ..., got: %s", excerpt)
	}
}

func TestBuildExcerpt_BothEllipses(t *testing.T) {
	// Match in the middle of a very long string.
	longText := strings.Repeat("before. ", 20) + "the target phrase in the middle " + strings.Repeat("after. ", 20)
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      longText,
	}

	excerpt := buildExcerpt(entry, []string{"target", "phrase"}, "target phrase")

	if !strings.HasPrefix(excerpt, "...") {
		t.Errorf("excerpt should start with ..., got: %s", excerpt)
	}
	if !strings.HasSuffix(excerpt, "...") {
		t.Errorf("excerpt should end with ..., got: %s", excerpt)
	}
}

func TestBuildExcerpt_NoMatch(t *testing.T) {
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      "no matching content here",
	}

	excerpt := buildExcerpt(entry, []string{"xyzzy_nonexistent"}, "xyzzy_nonexistent")
	if excerpt != "" {
		t.Errorf("expected empty excerpt, got: %s", excerpt)
	}
}

func TestBuildExcerpt_EmptyText(t *testing.T) {
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      "",
	}

	excerpt := buildExcerpt(entry, []string{"test"}, "test")
	if excerpt != "" {
		t.Errorf("expected empty excerpt for empty text, got: %s", excerpt)
	}
}

func TestBuildExcerpt_MatchAtStart(t *testing.T) {
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      "target phrase is at the very beginning of this text",
	}

	excerpt := buildExcerpt(entry, []string{"target", "phrase"}, "target phrase")

	if strings.HasPrefix(excerpt, "...") {
		t.Errorf("excerpt should not start with ... when match is at the beginning, got: %s", excerpt)
	}
}

func TestBuildExcerpt_MatchAtEnd(t *testing.T) {
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      "lots of text before and the target phrase is at the very end now",
	}

	excerpt := buildExcerpt(entry, []string{"target", "phrase"}, "target phrase")

	if strings.HasSuffix(excerpt, "...") {
		t.Errorf("excerpt should not end with ... when match is near the end, got: %s", excerpt)
	}
}

func TestBuildExcerpt_TermBracketing(t *testing.T) {
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      "the embedding index was broken and needed a complete rewrite",
	}

	excerpt := buildExcerpt(entry, []string{"embedding"}, "embedding")

	if !strings.Contains(excerpt, "[embedding]") {
		t.Errorf("excerpt should bracket matched term, got: %s", excerpt)
	}
}

func TestBuildExcerpt_PartialPhrase(t *testing.T) {
	// Query is multi-term phrase but only first term appears in text.
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      "the embedding was found but the index was not there",
	}

	excerpt := buildExcerpt(entry, []string{"embedding", "index"}, "embedding index")

	if !strings.Contains(excerpt, "[embedding]") {
		t.Errorf("should bracket 'embedding', got: %s", excerpt)
	}
	if !strings.Contains(excerpt, "[index]") {
		t.Errorf("should bracket 'index', got: %s", excerpt)
	}
}

// ---------------------------------------------------------------------------
// Test FormatResults
// ---------------------------------------------------------------------------

func TestFormatResults_Empty(t *testing.T) {
	output := FormatResults(nil)
	if output != "No matching sessions." {
		t.Errorf("expected 'No matching sessions.', got: %s", output)
	}

	output = FormatResults([]SearchResult{})
	if output != "No matching sessions." {
		t.Errorf("expected 'No matching sessions.' for empty slice, got: %s", output)
	}
}

func TestFormatResults_Single(t *testing.T) {
	results := []SearchResult{
		{
			SessionID:   "test-1",
			Name:        "Test Session",
			WorkingDir:  "/home/user",
			LastUpdated: makeNow(),
			TotalCost:   0.05,
			Excerpt:     "...the [embedding] index was [broken]...",
			MatchScore:  3,
		},
	}

	output := FormatResults(results)

	if !strings.Contains(output, "[2026-06-27]") {
		t.Errorf("expected date format [YYYY-MM-DD], got: %s", output)
	}
	if !strings.Contains(output, "Test Session") {
		t.Errorf("expected session name, got: %s", output)
	}
	if !strings.Contains(output, "/home/user") {
		t.Errorf("expected working dir, got: %s", output)
	}
	if !strings.Contains(output, "[embedding]") {
		t.Errorf("expected bracketed term in excerpt, got: %s", output)
	}

	// Should start with "  " for the excerpt line
	if !strings.Contains(output, "  ...") {
		t.Errorf("expected indented excerpt, got: %s", output)
	}
}

func TestFormatResults_Multiple(t *testing.T) {
	results := []SearchResult{
		{
			SessionID:   "test-1",
			Name:        "First",
			WorkingDir:  "/home/a",
			LastUpdated: makeNow(),
			Excerpt:     "...first match...",
			MatchScore:  3,
		},
		{
			SessionID:   "test-2",
			Name:        "Second",
			WorkingDir:  "/home/b",
			LastUpdated: makeYesterday(),
			Excerpt:     "...second match...",
			MatchScore:  2,
		},
	}

	output := FormatResults(results)

	// Results should be separated by a blank line.
	// Structure: "[date] Name — dir\n  excerpt\n\n[date] Name — dir\n  excerpt"
	lines := strings.Split(output, "\n")
	_ = lines // used for inspection

	// Verify blank line between results (the \n\n between results)
	if !strings.Contains(output, "\n\n") {
		t.Errorf("expected blank line between results, got:\n%s", output)
	}

	if !strings.Contains(output, "First") {
		t.Errorf("expected 'First', got: %s", output)
	}
	if !strings.Contains(output, "Second") {
		t.Errorf("expected 'Second', got: %s", output)
	}
}

func TestFormatResults_OutputStructure(t *testing.T) {
	results := []SearchResult{
		{
			SessionID:   "sid",
			Name:        "My Session",
			WorkingDir:  "/workspace",
			LastUpdated: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Excerpt:     "the quick brown fox",
		},
	}

	output := FormatResults(results)
	expected := "[2026-01-15] My Session — /workspace\n  the quick brown fox"
	if output != expected {
		t.Errorf("output mismatch.\nExpected: %q\nGot:      %q", expected, output)
	}
}

// ---------------------------------------------------------------------------
// Test tokenizeQuery
// ---------------------------------------------------------------------------

func TestTokenizeQuery(t *testing.T) {
	tests := []struct {
		query    string
		expected []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"  spaced  out  ", []string{"spaced", "out"}},
		{"single", []string{"single"}},
		{"", nil},
		{"UPPER lower", []string{"upper", "lower"}},
	}

	for _, tt := range tests {
		result := tokenizeQuery(strings.ToLower(tt.query))
		if len(result) != len(tt.expected) {
			t.Errorf("tokenizeQuery(%q): got %d tokens, want %d", tt.query, len(result), len(tt.expected))
			continue
		}
		for i, v := range tt.expected {
			if result[i] != v {
				t.Errorf("tokenizeQuery(%q)[%d] = %q, want %q", tt.query, i, result[i], v)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test passesFilters
// ---------------------------------------------------------------------------

func TestPassFilters_All(t *testing.T) {
	entry := SessionIndexEntry{
		SessionID:   "test",
		WorkingDir:  "/home/user",
		LastUpdated: time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC),
	}

	// No filters
	if !passFilters(entry, SearchOptions{}) {
		t.Error("should pass with no filters")
	}

	// WorkingDir match
	if !passFilters(entry, SearchOptions{WorkingDir: "/home/user"}) {
		t.Error("should pass with matching WorkingDir")
	}

	// WorkingDir mismatch
	if passFilters(entry, SearchOptions{WorkingDir: "/other"}) {
		t.Error("should fail with mismatched WorkingDir")
	}

	// Since before entry
	if !passFilters(entry, SearchOptions{Since: time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)}) {
		t.Error("should pass with Since before entry")
	}

	// Since after entry
	if passFilters(entry, SearchOptions{Since: time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)}) {
		t.Error("should fail with Since after entry")
	}

	// Until after entry
	if !passFilters(entry, SearchOptions{Until: time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)}) {
		t.Error("should pass with Until after entry")
	}

	// Until before entry
	if passFilters(entry, SearchOptions{Until: time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)}) {
		t.Error("should fail with Until before entry")
	}
}

// ---------------------------------------------------------------------------
// Test rankEntry
// ---------------------------------------------------------------------------

func TestRankEntry(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		query    string
		terms    []string
		expected int
	}{
		{
			name:     "exact phrase",
			text:     "the embedding index was broken",
			query:    "embedding index",
			terms:    []string{"embedding", "index"},
			expected: 3,
		},
		{
			name:     "all terms but not phrase",
			text:     "the embedding was broken and the index was gone",
			query:    "embedding index",
			terms:    []string{"embedding", "index"},
			expected: 2,
		},
		{
			name:     "one term",
			text:     "the embedding was broken",
			query:    "embedding index",
			terms:    []string{"embedding", "index"},
			expected: 1,
		},
		{
			name:     "no match",
			text:     "completely unrelated content",
			query:    "embedding index",
			terms:    []string{"embedding", "index"},
			expected: 0,
		},
		{
			name:     "empty query",
			text:     "some text",
			query:    "",
			terms:    nil,
			expected: 0,
		},
		{
			name:     "single term match",
			text:     "hello world",
			query:    "hello",
			terms:    []string{"hello"},
			expected: 3, // single word = exact phrase
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := SessionIndexEntry{Text: tt.text}
			got := rankEntry(entry, tt.query, tt.terms)
			if got != tt.expected {
				t.Errorf("rankEntry(%q, %q) = %d, want %d", tt.text, tt.query, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration: Search + FormatResults
// ---------------------------------------------------------------------------

func TestSearch_FormatIntegration(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: "embedding index", Limit: 2})
	output := FormatResults(results)

	if !strings.Contains(output, "Test Session One") {
		t.Errorf("output should contain 'Test Session One':\n%s", output)
	}
	if !strings.Contains(output, "Database Migration") {
		t.Errorf("output should contain 'Database Migration':\n%s", output)
	}
	if strings.Contains(output, "Auth Debugging") {
		t.Errorf("output should NOT contain 'Auth Debugging':\n%s", output)
	}

	// Check bracketed terms appear in output
	if !strings.Contains(output, "[embedding]") {
		t.Errorf("output should contain bracketed 'embedding':\n%s", output)
	}
}

func TestSearch_FormatIntegration_NoResults(t *testing.T) {
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: "nonexistent_thing_xyz"})
	output := FormatResults(results)

	if output != "No matching sessions." {
		t.Errorf("expected 'No matching sessions.', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Acceptance criteria tests (SP-083-2)
// ---------------------------------------------------------------------------

func TestSearch_RankingPhraseTier(t *testing.T) {
	// Criterion 1: query "exact phrase" — entries containing the literal phrase
	// rank above entries that only match individual words.
	idx := &SessionIndex{
		Sessions: map[string]SessionIndexEntry{
			"phrase-match": {
				SessionID:   "phrase-match",
				Name:        "Has Exact Phrase",
				WorkingDir:  "/home/user",
				LastUpdated: time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
				Text:        "this session covers the exact phrase that we are searching for today",
			},
			"terms-only": {
				SessionID:   "terms-only",
				Name:        "Has Individual Words",
				WorkingDir:  "/home/user",
				LastUpdated: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
				Text:        "this session has the word exact and later the word phrase but not adjacent",
			},
		},
	}

	results := Search(idx, SearchOptions{Query: "exact phrase"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// "phrase-match" has tier 3 (exact phrase), should be first despite being older
	if results[0].SessionID != "phrase-match" {
		t.Errorf("expected 'phrase-match' first (tier 3 phrase), got %s with score %d", results[0].SessionID, results[0].MatchScore)
	}
	if results[0].MatchScore != 3 {
		t.Errorf("expected tier 3 for phrase match, got %d", results[0].MatchScore)
	}

	// "terms-only" has tier 2 (both words present), should be second
	if results[1].SessionID != "terms-only" {
		t.Errorf("expected 'terms-only' second, got %s with score %d", results[1].SessionID, results[1].MatchScore)
	}
	if results[1].MatchScore != 2 {
		t.Errorf("expected tier 2 for terms-only, got %d", results[1].MatchScore)
	}
}

func TestSearch_RankingAllTermsTier(t *testing.T) {
	// Criterion 2: query "foo bar" — entries containing BOTH rank above
	// entries with only one.
	idx := &SessionIndex{
		Sessions: map[string]SessionIndexEntry{
			"both": {
				SessionID:   "both",
				Name:        "Has Both Terms",
				WorkingDir:  "/home/user",
				LastUpdated: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
				Text:        "foo appeared here and bar was also mentioned in the discussion",
			},
			"only-foo": {
				SessionID:   "only-foo",
				Name:        "Has Only Foo",
				WorkingDir:  "/home/user",
				LastUpdated: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
				Text:        "foo was the only thing discussed in this session",
			},
		},
	}

	results := Search(idx, SearchOptions{Query: "foo bar"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// "both" has tier 2 (all terms), should be first despite being older
	if results[0].SessionID != "both" {
		t.Errorf("expected 'both' first (tier 2), got %s with score %d", results[0].SessionID, results[0].MatchScore)
	}
	if results[0].MatchScore != 2 {
		t.Errorf("expected tier 2 for 'both', got %d", results[0].MatchScore)
	}

	// "only-foo" has tier 1 (one term), should be second despite being newer
	if results[1].SessionID != "only-foo" {
		t.Errorf("expected 'only-foo' second (tier 1), got %s with score %d", results[1].SessionID, results[1].MatchScore)
	}
	if results[1].MatchScore != 1 {
		t.Errorf("expected tier 1 for 'only-foo', got %d", results[1].MatchScore)
	}
}

func TestSearch_RankingAnyTermTier(t *testing.T) {
	// Criterion 3: query "foo baz" — entries with only "foo" rank below
	// those with both, but above zero-match entries (which are excluded).
	idx := &SessionIndex{
		Sessions: map[string]SessionIndexEntry{
			"both": {
				SessionID:   "both",
				Name:        "Has Both",
				WorkingDir:  "/home/user",
				LastUpdated: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
				Text:        "foo and baz were both discussed here",
			},
			"only-foo": {
				SessionID:   "only-foo",
				Name:        "Has Only Foo",
				WorkingDir:  "/home/user",
				LastUpdated: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
				Text:        "foo was mentioned several times but nothing else was discussed",
			},
			"none": {
				SessionID:   "none",
				Name:        "Has Neither",
				WorkingDir:  "/home/user",
				LastUpdated: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
				Text:        "completely unrelated session with no relevant terms",
			},
		},
	}

	results := Search(idx, SearchOptions{Query: "foo baz"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results (zero-match excluded), got %d", len(results))
	}

	// "both" has tier 2, should be first
	if results[0].SessionID != "both" {
		t.Errorf("expected 'both' first (tier 2), got %s with score %d", results[0].SessionID, results[0].MatchScore)
	}

	// "only-foo" has tier 1, should be second
	if results[1].SessionID != "only-foo" {
		t.Errorf("expected 'only-foo' second (tier 1), got %s with score %d", results[1].SessionID, results[1].MatchScore)
	}

	// "none" should not appear (score 0 = excluded)
	for _, r := range results {
		if r.SessionID == "none" {
			t.Error("zero-match entry should be excluded from results")
		}
	}
}

func TestSearch_TieBreakByRecency(t *testing.T) {
	// Criterion 4: two entries both match all terms; the more recently
	// updated one comes first.
	newer := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	older := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)

	idx := &SessionIndex{
		Sessions: map[string]SessionIndexEntry{
			"newer-entry": {
				SessionID:   "newer-entry",
				Name:        "Newer Session",
				WorkingDir:  "/home/user",
				LastUpdated: newer,
				Text:        "the embedding index was updated recently",
			},
			"older-entry": {
				SessionID:   "older-entry",
				Name:        "Older Session",
				WorkingDir:  "/home/user",
				LastUpdated: older,
				Text:        "the embedding index was created a while ago",
			},
		},
	}

	results := Search(idx, SearchOptions{Query: "embedding index"})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Both have tier 3 (exact phrase), so recency decides
	if results[0].SessionID != "newer-entry" {
		t.Errorf("expected 'newer-entry' first (more recent), got %s", results[0].SessionID)
	}
	if results[1].SessionID != "older-entry" {
		t.Errorf("expected 'older-entry' second, got %s", results[1].SessionID)
	}
}

func TestSearch_WorkingDirFilter(t *testing.T) {
	// Criterion 5: only entries whose WorkingDir matches appear.
	idx := &SessionIndex{
		Sessions: map[string]SessionIndexEntry{
			"proj-a": {
				SessionID:   "proj-a",
				Name:        "Project A Session",
				WorkingDir:  "/home/user/project-a",
				LastUpdated: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
				Text:        "debugging the authentication module",
			},
			"proj-b": {
				SessionID:   "proj-b",
				Name:        "Project B Session",
				WorkingDir:  "/home/user/project-b",
				LastUpdated: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
				Text:        "working on authentication changes here too",
			},
			"proj-a-2": {
				SessionID:   "proj-a-2",
				Name:        "Project A Session 2",
				WorkingDir:  "/home/user/project-a",
				LastUpdated: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
				Text:        "authentication was discussed again",
			},
		},
	}

	results := Search(idx, SearchOptions{
		Query:      "authentication",
		WorkingDir: "/home/user/project-a",
	})

	if len(results) != 2 {
		t.Fatalf("expected 2 results from project-a, got %d", len(results))
	}

	for _, r := range results {
		if r.WorkingDir != "/home/user/project-a" {
			t.Errorf("all results should have WorkingDir='/home/user/project-a', got %s for %s", r.WorkingDir, r.SessionID)
		}
	}
}

func TestSearch_DateFilter(t *testing.T) {
	// Criterion 6: Since / Until filter correctly.
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	yesterday := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	lastWeek := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	idx := &SessionIndex{
		Sessions: map[string]SessionIndexEntry{
			"now": {
				SessionID:   "now",
				Name:        "Now Session",
				WorkingDir:  "/home/user",
				LastUpdated: now,
				Text:        "search term appears here",
			},
			"yesterday": {
				SessionID:   "yesterday",
				Name:        "Yesterday Session",
				WorkingDir:  "/home/user",
				LastUpdated: yesterday,
				Text:        "search term also here",
			},
			"old": {
				SessionID:   "old",
				Name:        "Old Session",
				WorkingDir:  "/home/user",
				LastUpdated: lastWeek,
				Text:        "search term was mentioned long ago",
			},
		},
	}

	// Since: only entries >= cutoff (now + yesterday pass, old is filtered)
	sinceCutoff := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	results := Search(idx, SearchOptions{
		Query: "search term",
		Since: sinceCutoff,
	})
	if len(results) != 2 {
		t.Fatalf("Since filter: expected 2 results, got %d", len(results))
	}

	// Until: only entries <= deadline — use midnight June 25 to exclude
	// yesterday and now, keeping only old.
	untilCutoff := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	results = Search(idx, SearchOptions{
		Query: "search term",
		Until: untilCutoff,
	})
	if len(results) != 1 {
		t.Fatalf("Until filter: expected 1 result, got %d", len(results))
	}
	if results[0].SessionID != "old" {
		t.Errorf("expected 'old', got %s", results[0].SessionID)
	}

	// Since + Until combined: only yesterday (between June 25 and June 27)
	results = Search(idx, SearchOptions{
		Query: "search term",
		Since: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 6, 26, 23, 59, 59, 0, time.UTC),
	})
	if len(results) != 1 {
		t.Fatalf("Combined filter: expected 1 result, got %d", len(results))
	}
	if results[0].SessionID != "yesterday" {
		t.Errorf("expected 'yesterday', got %s", results[0].SessionID)
	}
}

func TestSearch_ExcerptBrackets(t *testing.T) {
	// Criterion 8: an excerpt around "the matching term" should have each
	// matched term wrapped in [brackets].
	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      "some context before the matching term appears here and continues after",
	}

	terms := []string{"the", "matching", "term"}
	queryLower := "the matching term"

	excerpt := buildExcerpt(entry, terms, queryLower)

	// Each matched term should be bracketed
	for _, term := range terms {
		bracketed := "[" + term + "]"
		if !strings.Contains(excerpt, bracketed) {
			t.Errorf("excerpt should contain %s, got: %s", bracketed, excerpt)
		}
	}

	// Verify brackets are actually in the output
	if !strings.Contains(excerpt, "[the]") {
		t.Errorf("missing [the] in excerpt: %s", excerpt)
	}
	if !strings.Contains(excerpt, "[matching]") {
		t.Errorf("missing [matching] in excerpt: %s", excerpt)
	}
	if !strings.Contains(excerpt, "[term]") {
		t.Errorf("missing [term] in excerpt: %s", excerpt)
	}
}

func TestSearch_ExcerptLength(t *testing.T) {
	// Criterion 9: excerpts are at most ~250 chars.
	longText := strings.Repeat("padding text with extra words to make this very long. ", 50) +
		"the target phrase we are searching for" +
		strings.Repeat(" more trailing content to extend the text far beyond the window. ", 50)

	entry := SessionIndexEntry{
		SessionID: "test",
		Text:      longText,
	}

	excerpt := buildExcerpt(entry, []string{"target", "phrase"}, "target phrase")

	if len(excerpt) > 250 {
		t.Errorf("excerpt length %d exceeds 250 chars: %s", len(excerpt), excerpt)
	}

	// Verify it's not trivially empty
	if excerpt == "" {
		t.Error("excerpt should not be empty for a valid match")
	}
}

func TestSearch_NoMatches(t *testing.T) {
	// Criterion 11: returns empty slice.
	idx := buildTestIndex(t)
	results := Search(idx, SearchOptions{Query: "xyzzy_nonexistent_term"})

	if results != nil && len(results) != 0 {
		t.Errorf("expected empty results for no matches, got %d results", len(results))
	}
}

func TestFormatResults_HumanReadable(t *testing.T) {
	// Criterion 12: output has the format
	// [date] name — working_dir\n  excerpt\n
	// per result.
	results := []SearchResult{
		{
			SessionID:   "abc-123",
			Name:        "My Test Session",
			WorkingDir:  "/home/dev/myproject",
			LastUpdated: time.Date(2026, 03, 15, 14, 30, 0, 0, time.UTC),
			TotalCost:   0.05,
			Excerpt:     "...the [embedding] index was [broken]...",
			MatchScore:  3,
		},
	}

	output := FormatResults(results)

	// Verify date format [YYYY-MM-DD]
	if !strings.Contains(output, "[2026-03-15]") {
		t.Errorf("expected date [2026-03-15], got: %s", output)
	}

	// Verify session name
	if !strings.Contains(output, "My Test Session") {
		t.Errorf("expected session name, got: %s", output)
	}

	// Verify em dash separator
	if !strings.Contains(output, "—") {
		t.Errorf("expected em dash separator '—', got: %s", output)
	}

	// Verify working directory
	if !strings.Contains(output, "/home/dev/myproject") {
		t.Errorf("expected working dir, got: %s", output)
	}

	// Verify indented excerpt (two spaces before excerpt)
	if !strings.Contains(output, "  ...the [embedding]") {
		t.Errorf("expected indented excerpt with '  ' prefix, got: %s", output)
	}

	// Verify the exact structure: "[date] name — dir\n  excerpt"
	expected := "[2026-03-15] My Test Session — /home/dev/myproject\n  ...the [embedding] index was [broken]..."
	if output != expected {
		t.Errorf("format mismatch.\nExpected: %q\nGot:      %q", expected, output)
	}
}
