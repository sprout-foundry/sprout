package commands

import (
	"strings"
)

// parseDiffForContent extracts old and new content from unified git diff
func parseDiffForContent(diffOutput, filename string) (string, string) {
	oldLines := []string{}
	newLines := []string{}
	inDiff := false
	inOldSection := false
	inNewSection := false

	lines := strings.Split(diffOutput, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			inDiff = true
			if strings.Contains(line, filename) {
				inOldSection = false
				inNewSection = false
			}
			continue
		}
		if !inDiff {
			continue
		}
		if strings.HasPrefix(line, "@ @") {
			inOldSection = false
			inNewSection = false
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			if !inOldSection {
				inOldSection = true
				inNewSection = false
			}
			oldLines = append(oldLines, strings.TrimPrefix(line, "- "))
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			if !inNewSection {
				inNewSection = true
				inOldSection = false
			}
			newLines = append(newLines, strings.TrimPrefix(line, "+ "))
		} else if !strings.HasPrefix(line, " ") {
			// Reset sections on other diff markers
			inOldSection = false
			inNewSection = false
		} else {
			// Unchanged lines (space prefix)
			if inOldSection {
				oldLines = append(oldLines, strings.TrimPrefix(line, " "))
			}
			if inNewSection {
				newLines = append(newLines, strings.TrimPrefix(line, " "))
			}
		}
	}

	return strings.Join(oldLines, "\n") + "\n", strings.Join(newLines, "\n") + "\n"
}
