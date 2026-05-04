package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckDuplicatesResult holds the result of a duplicate check.
type CheckDuplicatesResult struct {
	// Duplicates is a list of potential duplicate matches, sorted by similarity.
	Duplicates []QueryResult
	// WarningText is a formatted warning message for the agent, or empty if no duplicates.
	WarningText string
}

// FormatDuplicateWarning formats duplicate matches as a warning string.
func FormatDuplicateWarning(matches []QueryResult) string {
	if len(matches) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("⚠ Potential duplicate detected:\n")
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("  • %s (similarity: %.2f) — %s\n",
			m.Record.ID, m.Similarity, m.Record.Signature))
	}
	sb.WriteString("Consider whether the new code duplicates existing functionality.\n")
	return sb.String()
}

// CheckFileForDuplicates checks if any functions in the given file content
// have semantically similar existing code in the index.
//
// It works by extracting code units from the content, embedding each one,
// and querying the existing index for similar records. Self-matches (same
// ID or same file path) are filtered out.
//
// The top-K overall matches above the threshold are returned, sorted by
// similarity descending.
func CheckFileForDuplicates(ctx context.Context, mgr *IndexManager, filePath string, content string, threshold float32, topK int) (*CheckDuplicatesResult, error) {
	if mgr == nil {
		return &CheckDuplicatesResult{}, nil
	}

	// If threshold is not specified, use the default.
	if threshold == 0 {
		threshold = 0.90
	}
	if topK <= 0 {
		topK = 3
	}

	units, err := extractFromContent(filePath, content)
	if err != nil {
		return nil, fmt.Errorf("embedding: extract units from %s: %w", filePath, err)
	}

	if len(units) == 0 {
		return &CheckDuplicatesResult{}, nil
	}

	// Query for each code unit and collect matches.
	var allMatches []QueryResult
	for _, u := range units {
		// Build the embedding text the same way as the index pipeline.
		queryText := u.Signature + "\n" + u.Body

		matches, err := mgr.QuerySimilar(ctx, queryText, topK, threshold)
		if err != nil {
			// Log but continue — a single unit failure shouldn't block the whole check.
			continue
		}

		// Filter out self-matches: same ID or same file path.
		for _, m := range matches {
			if m.Record.ID == u.ID || m.Record.File == filePath {
				continue
			}
			allMatches = append(allMatches, m)
		}
	}

	// Deduplicate by Record.ID (the same indexed record could match multiple units).
	allMatches = deduplicateMatches(allMatches)

	// Sort by similarity descending.
	sortMatchesBySimilarityDesc(allMatches)

	// Trim to topK.
	if len(allMatches) > topK {
		allMatches = allMatches[:topK]
	}

	result := &CheckDuplicatesResult{
		Duplicates:  allMatches,
		WarningText: FormatDuplicateWarning(allMatches),
	}

	return result, nil
}

// extractFromContent writes content to a temporary file with the appropriate
// extension so that ExtractFromFile can parse it, then extracts code units.
// The temporary file is cleaned up after extraction.
func extractFromContent(filePath string, content string) ([]CodeUnit, error) {
	// Create a temp file with the same extension as the target path so the
	// extractor picks the right language parser.
	tmpDir, err := os.MkdirTemp("", "embedding-check-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpPath := filepath.Join(tmpDir, filepath.Base(filePath))
	if filepath.Ext(tmpPath) == "" {
		// Fallback: add .go if no extension (most common case)
		tmpPath += ".go"
	}

	if err := os.WriteFile(tmpPath, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("write temp file: %w", err)
	}

	return ExtractFromFile(tmpPath)
}

// deduplicateMatches removes duplicate QueryResults based on Record.ID,
// keeping the entry with the highest similarity score.
func deduplicateMatches(matches []QueryResult) []QueryResult {
	best := make(map[string]QueryResult)
	var order []string

	for _, m := range matches {
		if existing, ok := best[m.Record.ID]; !ok {
			best[m.Record.ID] = m
			order = append(order, m.Record.ID)
		} else if m.Similarity > existing.Similarity {
			best[m.Record.ID] = m
		}
	}

	result := make([]QueryResult, 0, len(best))
	for _, id := range order {
		result = append(result, best[id])
	}

	return result
}

// sortMatchesBySimilarityDesc sorts matches in-place by similarity descending.
func sortMatchesBySimilarityDesc(matches []QueryResult) {
	for i := 1; i < len(matches); i++ {
		key := matches[i]
		j := i - 1
		for j >= 0 && matches[j].Similarity < key.Similarity {
			matches[j+1] = matches[j]
			j--
		}
		matches[j+1] = key
	}
}
