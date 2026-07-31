package tools

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fetchTempDir is the directory for cached fetch_url content files.
// Matches the agent convention of /tmp/sprout/ for transient files.
const fetchTempDir = "/tmp/sprout/fetch"

// fetchContentThreshold is the character count above which content is
// saved to a temp file with a section TOC instead of returned inline.
const fetchContentThreshold = 5000

// saveFetchContent writes fetched content to a deterministic temp file
// keyed by URL hash. Returns the file path for the agent to read later.
func saveFetchContent(url string, content string) (string, error) {
	if err := os.MkdirAll(fetchTempDir, 0755); err != nil {
		return "", fmt.Errorf("create fetch temp dir: %w", err)
	}

	// Deterministic filename from URL hash — repeated fetches reuse the file.
	hash := sha256.Sum256([]byte(url))
	filename := fmt.Sprintf("fetch_%x.txt", hash[:8])
	path := filepath.Join(fetchTempDir, filename)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write fetch content to %s: %w", path, err)
	}

	return path, nil
}

// buildSectionTOC parses markdown headers from content and builds a
// table of contents with line number ranges for each section.
// Returns a formatted TOC string the agent can use with read_file.
func buildSectionTOC(content string) string {
	type section struct {
		level   int
		title   string
		startLn int // 1-based line number where section begins
	}

	var sections []section
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Match markdown headers: # Title, ## Subtitle, etc.
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			// Must have a space after the #s (not just ### alone)
			if level >= 1 && level < 4 && len(trimmed) > level && trimmed[level] == ' ' {
				title := strings.TrimSpace(trimmed[level:])
				sections = append(sections, section{
					level:   level,
					title:   title,
					startLn: i + 1, // 1-based
				})
			}
		}
	}

	if len(sections) == 0 {
		return ""
	}

	// Build TOC with line ranges.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Page has %d sections across %d lines (%d chars).\n",
		len(sections), len(lines), len(content)))
	sb.WriteString("Use read_file with view_range to read specific sections:\n\n")

	for i, sec := range sections {
		endLn := len(lines)
		if i+1 < len(sections) {
			endLn = sections[i+1].startLn - 1
		}
		indent := strings.Repeat("  ", sec.level-1)
		lineCount := endLn - sec.startLn + 1
		sb.WriteString(fmt.Sprintf("%s- **%s** (lines %d–%d, %d lines)\n",
			indent, sec.title, sec.startLn, endLn, lineCount))
	}

	sb.WriteString(fmt.Sprintf("\nFull content saved to: (see file path below)\n"))

	return sb.String()
}
