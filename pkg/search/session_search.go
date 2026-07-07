// Package search provides cross-session search with ranking and result formatting.
//
// This file implements the search query engine, tiered ranking, excerpt
// generation with bracketed terms, and human-readable formatting.  It is
// designed as a library — no CLI or WebUI dependencies.

package search

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// SearchOptions configures a search query against a SessionIndex.
type SearchOptions struct {
	// Query is the search string (required).  Whitespace-separated tokens
	// are treated as individual terms; the full query is also tested as an
	// exact phrase.
	Query string

	// WorkingDir restricts results to entries whose WorkingDir exactly
	// matches this value.  Empty string means no filter.
	WorkingDir string

	// Since limits results to entries with LastUpdated >= Since.  Zero
	// value disables the filter.
	Since time.Time

	// Until limits results to entries with LastUpdated <= Until.  Zero
	// value disables the filter.
	Until time.Time

	// Limit caps the number of returned results.  Zero uses the default
	// of 20.
	Limit int
}

// SearchResult is a single matched session with a formatted excerpt.
type SearchResult struct {
	SessionID   string    `json:"session_id"`
	Name        string    `json:"name"`
	WorkingDir  string    `json:"working_directory"`
	LastUpdated time.Time `json:"last_updated"`
	TotalCost   float64   `json:"total_cost"`
	Excerpt     string    `json:"excerpt"`
	MatchScore  int       `json:"match_score"` // 1 (any term), 2 (all terms), 3 (exact phrase)
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

const (
	defaultLimit  = 20
	excerptBefore = 80
	excerptAfter  = 120
)

// Search runs the query against the index and returns ranked results.
//
// Results are scored in three tiers (higher is better):
//
//   - 3: the full query phrase appears in the entry's text.
//   - 2: every whitespace-separated term appears (but not necessarily adjacent).
//   - 1: at least one term appears.
//
// Ties are broken by recency (newer LastUpdated first).  Filters
// (WorkingDir, Since, Until) are applied before ranking so that only
// eligible entries are considered.
func Search(idx *SessionIndex, opts SearchOptions) []SearchResult {
	if idx == nil || idx.Sessions == nil {
		return nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	terms := tokenizeQuery(opts.Query)
	queryLower := strings.ToLower(opts.Query)

	type candidate struct {
		entry SessionIndexEntry
		score int
	}

	var candidates []candidate

	for _, entry := range idx.Sessions {
		if !passFilters(entry, opts) {
			continue
		}

		score := rankEntry(entry, queryLower, terms)
		if score == 0 {
			continue
		}

		candidates = append(candidates, candidate{entry: entry, score: score})
	}

	// Sort: score descending, then LastUpdated descending, then SessionID ascending.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if !candidates[i].entry.LastUpdated.Equal(candidates[j].entry.LastUpdated) {
			return candidates[i].entry.LastUpdated.After(candidates[j].entry.LastUpdated)
		}
		return candidates[i].entry.SessionID < candidates[j].entry.SessionID
	})

	// Build results with excerpts, capping at limit.
	n := len(candidates)
	if n > limit {
		n = limit
	}

	results := make([]SearchResult, 0, n)
	for i := 0; i < n; i++ {
		c := candidates[i]
		excerpt := buildExcerpt(c.entry, terms, queryLower)
		results = append(results, SearchResult{
			SessionID:   c.entry.SessionID,
			Name:        c.entry.Name,
			WorkingDir:  c.entry.WorkingDir,
			LastUpdated: c.entry.LastUpdated,
			TotalCost:   c.entry.TotalCost,
			Excerpt:     excerpt,
			MatchScore:  c.score,
		})
	}

	return results
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

func passFilters(entry SessionIndexEntry, opts SearchOptions) bool {
	if opts.WorkingDir != "" && entry.WorkingDir != opts.WorkingDir {
		return false
	}
	if !opts.Since.IsZero() && entry.LastUpdated.Before(opts.Since) {
		return false
	}
	if !opts.Until.IsZero() && entry.LastUpdated.After(opts.Until) {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Ranking
// ---------------------------------------------------------------------------

// rankEntry returns the tier score (0–3) for an entry against the query.
func rankEntry(entry SessionIndexEntry, queryLower string, terms []string) int {
	if queryLower == "" {
		return 0
	}

	// Tier 3: exact phrase match.
	if strings.Contains(entry.Text, queryLower) {
		return 3
	}

	// Check which terms appear using word-boundary matching.
	matchCount := 0
	for _, t := range terms {
		if matchWholeWord(entry.Text, t) {
			matchCount++
		}
	}

	if matchCount == len(terms) {
		return 2 // all terms present
	}
	if matchCount > 0 {
		return 1 // at least one term present
	}
	return 0
}

// matchWholeWord returns true if term appears as a whole word in text.
// Uses \b word-boundary regex so "test" does not match "testing".
func matchWholeWord(text, term string) bool {
	re := wordBoundaryRegex(term)
	return re.MatchString(text)
}

// wordBoundaryRegex compiles a regex that matches the term as a whole word.
// The term should already be lowercased if case-insensitive matching is desired.
func wordBoundaryRegex(term string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(term) + `\b`)
}

// ---------------------------------------------------------------------------
// Query tokenization
// ---------------------------------------------------------------------------

// tokenizeQuery splits the query on whitespace and lowercases each token,
// filtering out empty strings.
func tokenizeQuery(query string) []string {
	fields := strings.Fields(query)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		t := strings.ToLower(f)
		if t != "" {
			terms = append(terms, t)
		}
	}
	return terms
}

// ---------------------------------------------------------------------------
// Excerpt generation
// ---------------------------------------------------------------------------

// buildExcerpt creates a highlighted excerpt from the entry's text.
//
// The window is centered around the first match position (firstMatchPos - excerptBefore to
// firstMatchPos + excerptAfter), then trimmed to space boundaries.  Matched terms are
// wrapped in [brackets] and "..." is added at truncated edges.
func buildExcerpt(entry SessionIndexEntry, terms []string, queryLower string) string {
	if entry.Text == "" {
		return ""
	}

	// Find the first match position.  Check phrase first, then first matching
	// term using word-boundary matching (for consistent positioning).
	firstPos := -1
	if queryLower != "" && strings.Contains(entry.Text, queryLower) {
		firstPos = strings.Index(entry.Text, queryLower)
	} else {
		for _, t := range terms {
			re := wordBoundaryRegex(t)
			loc := re.FindStringIndex(entry.Text)
			if loc != nil && (firstPos < 0 || loc[0] < firstPos) {
				firstPos = loc[0]
			}
		}
	}

	if firstPos < 0 {
		// No match found (shouldn't happen if the entry was scored > 0, but
		// be defensive).
		return ""
	}

	// Compute raw window.
	textLen := len(entry.Text)
	start := firstPos - excerptBefore
	if start < 0 {
		start = 0
	}
	end := firstPos + excerptAfter
	if end > textLen {
		end = textLen
	}

	// Trim to space boundaries.
	prefixDots := false
	suffixDots := false

	// Trim start backwards to nearest space.
	if start > 0 {
		sp := strings.LastIndexByte(entry.Text[:start], ' ')
		if sp >= 0 {
			prefixDots = true
			start = sp + 1
		}
	}

	// Trim end forwards to nearest space.
	if end < textLen {
		sp := strings.IndexByte(entry.Text[end:], ' ')
		if sp >= 0 {
			suffixDots = true
			end += sp
		}
	}

	// Safety clamp.
	if start < 0 {
		start = 0
	}
	if end > textLen {
		end = textLen
	}
	if start >= end {
		return ""
	}

	excerpt := entry.Text[start:end]

	// Determine which terms actually appear in the excerpt and bracket them.
	// Process from right to left so offsets stay valid.
	matched := findMatchedTermsInExcerpt(excerpt, terms)
	excerpt = bracketTerms(excerpt, matched)

	// Add ellipsis markers.
	if prefixDots {
		excerpt = "..." + excerpt
	}
	if suffixDots {
		excerpt = excerpt + "..."
	}

	return excerpt
}

// findMatchedTermsInExcerpt returns the subset of query terms that appear as
// whole words in the excerpt text, ordered by their first occurrence position
// (descending) so they can be bracketed right-to-left.
func findMatchedTermsInExcerpt(excerpt string, terms []string) []string {
	type termPos struct {
		term string
		pos  int
	}

	var found []termPos
	for _, t := range terms {
		re := wordBoundaryRegex(t)
		loc := re.FindStringIndex(excerpt)
		if loc != nil {
			found = append(found, termPos{term: t, pos: loc[0]})
		}
	}

	// Sort descending by position for right-to-left processing.
	sort.Slice(found, func(i, j int) bool {
		return found[i].pos > found[j].pos
	})

	result := make([]string, 0, len(found))
	for _, tp := range found {
		result = append(result, tp.term)
	}
	return result
}

// bracketTerms wraps each matched term in [brackets] within the text.  The
// matched slice should be ordered right-to-left (highest offset first) so
// that replacements don't invalidate earlier offsets.  Uses word-boundary
// regex to avoid matching partial words (e.g. "test" won't match "testing").
func bracketTerms(text string, matched []string) string {
	for _, t := range matched {
		re := wordBoundaryRegex(t)
		text = re.ReplaceAllString(text, "["+t+"]")
	}
	return text
}

// ---------------------------------------------------------------------------
// FormatResults
// ---------------------------------------------------------------------------

// FormatResults renders search results as a human-readable text block suitable
// for CLI output.  Each result is formatted as:
//
//	[YYYY-MM-DD] name — working_dir
//	  excerpt
//
// Results are separated by a single blank line.  If the slice is empty, the
// function returns "No matching sessions."
func FormatResults(results []SearchResult) string {
	if len(results) == 0 {
		return "No matching sessions."
	}

	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("[%s] %s — %s", r.LastUpdated.Format("2006-01-02"), r.Name, r.WorkingDir))
		sb.WriteString("\n")
		sb.WriteString("  ")
		// Replace newlines with spaces so multi-line excerpts don't break the
		// indented layout of the result block.
		sb.WriteString(strings.ReplaceAll(r.Excerpt, "\n", " "))
	}
	return sb.String()
}
