package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MemorySearchResult holds a single result from a text-based memory search.
type MemorySearchResult struct {
	Name    string
	Preview string
	Score   float64
	Content string
}

// SearchMemoriesByText lists all memory files and scores them against the query
// using simple text matching.
func SearchMemoriesByText(query string, topK int, threshold float64) ([]MemorySearchResult, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return nil, nil // No memory directory = no results, not an error
	}

	entries, err := readDirCompat(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memories directory: %w", err)
	}

	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	var results []MemorySearchResult

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		contentPath := filepath.Join(memoryDir, entry.Name())

		contentBytes, err := os.ReadFile(contentPath)
		if err != nil {
			continue // Skip unreadable files
		}
		content := string(contentBytes)

		// Get preview (first line or first 120 chars)
		preview := firstLine(content)
		if len(preview) > 120 {
			preview = truncateRunes(preview, 117)
		}

		// Score based on name match + content match
		score := scoreMemoryMatch(name, preview, content, queryWords)

		if score >= threshold {
			results = append(results, MemorySearchResult{
				Name:    name,
				Preview: preview,
				Score:   score,
				Content: content,
			})
		}
	}

	// Sort by score descending (simple bubble sort for small lists)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Limit to topK
	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// scoreMemoryMatch computes a relevance score (0.0-1.0) for a memory against the query.
//
// Each query word is worth 1.0 if it matches the memory name, 0.9 if it
// matches only the first-line preview, and 0.8 if it appears anywhere in the
// content body. A word matched by multiple fields takes the single best
// field weight, and the memory's score is the average across scored words
// (words under two characters are skipped entirely). The content weight is
// deliberately above the 0.75 default search threshold: the common
// natural-language case — every query word present in the body but not the
// title — must still surface the memory instead of filtering it out.
func scoreMemoryMatch(name, preview, content string, queryWords []string) float64 {
	if len(queryWords) == 0 {
		return 0
	}

	nameLower := strings.ToLower(name)
	previewLower := strings.ToLower(preview)
	contentLower := strings.ToLower(content)

	totalScore := float64(0)
	scoredWords := 0
	for _, word := range queryWords {
		if len(word) < 2 {
			// Skip stop-words too short to be meaningful; they are also
			// excluded from the denominator below.
			continue
		}
		scoredWords++

		switch {
		case strings.Contains(nameLower, word):
			totalScore += 1.0
		case strings.Contains(previewLower, word):
			totalScore += 0.9
		case strings.Contains(contentLower, word):
			totalScore += 0.8
		}
	}

	if scoredWords == 0 {
		return 0
	}
	return totalScore / float64(scoredWords)
}

// firstLine extracts the first non-empty line from content.
func firstLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// FormatMemorySearchResults formats search results for display.
func FormatMemorySearchResults(query string, results []MemorySearchResult, threshold float64) string {
	if len(results) == 0 {
		return fmt.Sprintf("No memories found matching: %q\n\nTry broadening your search or lowering the threshold (currently %.2f).", query, threshold)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d memory/memories matching: %q\n\n", len(results), query)

	for i, r := range results {
		fmt.Fprintf(&sb, "#%d — **%s** (relevance: %.2f)\n", i+1, r.Name, r.Score)
		if r.Preview != "" {
			fmt.Fprintf(&sb, "   Preview: %s\n", r.Preview)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Use `manage_memory` with operation=\"read\" to view the full content of any memory.")
	return sb.String()
}
