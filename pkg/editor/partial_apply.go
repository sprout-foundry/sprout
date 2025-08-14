package editor

import "strings"

// applyPartialEdit applies the updated section to the original content
// Improved version that handles partial code snippets better and cleans up comments
func applyPartialEdit(originalContent, updatedSection string, startLine, endLine int) string {
	lines := strings.Split(originalContent, "\n")

	// Clean the updated section of problematic comments that could cause issues
	cleanedUpdatedSection := cleanPartialCodeSnippet(updatedSection)
	updatedLines := strings.Split(cleanedUpdatedSection, "\n")

	// Validate and adjust the line range to avoid issues
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	if startLine > endLine {
		// If range is invalid, try to find a better insertion point
		betterStart, betterEnd := findBetterInsertionPoint(lines, updatedLines, startLine)
		startLine = betterStart
		endLine = betterEnd
	}

	// Replace the lines from startLine to endLine with the updated section (idempotent guard)
	before := lines[:startLine]
	after := lines[endLine+1:]

	// Idempotent guard: avoid duplicate insertion if updated already present
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, cleanedUpdatedSection) {
		return joined
	}

	// Combine: before + updated + after
	result := append(before, updatedLines...)
	result = append(result, after...)

	return strings.Join(result, "\n")
}

// cleanPartialCodeSnippet removes problematic comments and markers from partial code
func cleanPartialCodeSnippet(code string) string {
	lines := strings.Split(code, "\n")
	var cleanedLines []string

	for _, line := range lines {
		lineToCheck := strings.ToLower(strings.TrimSpace(line))

		// Skip obvious placeholder comments that could cause issues
		problematicComments := []string{
			"// existing code",
			"// unchanged",
			"// rest of",
			"// other functions",
			"// previous code",
			"// ... (truncated)",
			"/* existing",
			"/* unchanged",
		}

		shouldSkip := false
		for _, problematic := range problematicComments {
			if strings.Contains(lineToCheck, problematic) {
				shouldSkip = true
				break
			}
		}

		if !shouldSkip {
			cleanedLines = append(cleanedLines, line)
		}
	}

	return strings.Join(cleanedLines, "\n")
}

// findBetterInsertionPoint tries to find a more appropriate place to insert code
// when the provided line range is invalid or problematic
func findBetterInsertionPoint(originalLines, updatedLines []string, preferredStart int) (int, int) {
	// If we have function-like content in the update, try to find where it belongs
	firstUpdatedLine := ""
	if len(updatedLines) > 0 {
		firstUpdatedLine = strings.TrimSpace(updatedLines[0])
	}

	// For Go code, try to place functions in appropriate locations
	if strings.HasPrefix(firstUpdatedLine, "func ") {
		// Find other function definitions to place this near
		for i, line := range originalLines {
			if strings.Contains(strings.TrimSpace(line), "func ") && i >= preferredStart {
				// Insert before this function
				return i, i
			}
		}
	}

	// For imports, place with other imports
	if strings.HasPrefix(firstUpdatedLine, "import ") || strings.Contains(firstUpdatedLine, `"`) {
		for i, line := range originalLines {
			if strings.Contains(line, "import") {
				// Insert after existing imports
				return i + 1, i + 1
			}
		}
	}

	// Default: try to place at the end of the file before the last few lines
	safeEnd := len(originalLines) - 3
	if safeEnd < 0 {
		safeEnd = 0
	}

	return safeEnd, safeEnd
}
