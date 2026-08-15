package tools

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
)

// fetchTempDir returns the directory for cached fetch_url content files.
// It is rooted at os.TempDir() so it is writable on every supported platform
// — including Termux, where /tmp is not writable but $TMPDIR (the value
// os.TempDir() resolves to) is. On plain Linux this still yields
// /tmp/sprout/fetch. The read-back path-tier classifier
// (filesystem.IsUnderTmpPath) already treats os.TempDir() paths as
// unconditionally allowed, so cached files remain readable without prompting.
func fetchTempDir() string {
	return filepath.Join(os.TempDir(), "sprout", "fetch")
}

// fetchContentThreshold is the character count above which content is
// saved to a temp file with a section TOC instead of returned inline.
const fetchContentThreshold = 5000

// maxFetchFiles caps the number of cached fetch files to prevent unbounded
// disk usage. When the directory exceeds this count, the oldest files are
// evicted before writing new ones.
const maxFetchFiles = 100

// fetchTempCounter provides unique temp filenames to avoid races when
// multiple goroutines write to the same target path simultaneously.
var fetchTempCounter atomic.Uint64

// saveFetchContent writes fetched content to a deterministic temp file
// keyed by URL hash. Uses atomic write (tmp + rename) to prevent corruption
// from concurrent fetches of the same URL. Returns the file path for the
// agent to read later.
func saveFetchContent(url string, content string) (string, error) {
	dir := fetchTempDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create fetch temp dir: %w", err)
	}

	// Deterministic filename from URL hash — repeated fetches reuse the file.
	hash := sha256.Sum256([]byte(url))
	filename := fmt.Sprintf("fetch_%x.txt", hash[:8])
	path := filepath.Join(dir, filename)

	// Evict oldest files if we're over the cap.
	evictOldFiles(path)

	// Atomic write: use a unique temp filename so concurrent goroutines
	// writing to the same target don't stomp each other's temp file.
	tmpPath := fmt.Sprintf("%s.tmp.%d", path, fetchTempCounter.Add(1))
	if err := os.WriteFile(tmpPath, []byte(content), 0600); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("write fetch content to %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("rename fetch content to %s: %w", path, err)
	}

	return path, nil
}

// evictOldFiles removes the oldest files in the fetch temp dir when the count
// exceeds maxFetchFiles. Skips the targetPath itself and any .tmp files.
func evictOldFiles(targetPath string) {
	dir := fetchTempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Collect evictable files (not the target, not temp files).
	var evictable []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") {
			continue
		}
		if filepath.Join(dir, name) == targetPath {
			continue
		}
		evictable = append(evictable, e)
	}

	if len(evictable) < maxFetchFiles {
		return
	}

	// Sort by modification time (oldest first).
	sort.Slice(evictable, func(i, j int) bool {
		iInfo, iErr := evictable[i].Info()
		jInfo, jErr := evictable[j].Info()
		if iErr != nil || jErr != nil {
			return false
		}
		return iInfo.ModTime().Before(jInfo.ModTime())
	})

	// Remove enough to get under the cap.
	removeCount := len(evictable) - maxFetchFiles
	for i := 0; i < removeCount; i++ {
		os.Remove(filepath.Join(dir, evictable[i].Name()))
	}
}

// buildSectionTOC parses markdown headers from content and builds a
// table of contents with line number ranges for each section.
// Returns a formatted TOC string the agent can use with read_file.
// For content without headers, returns a fallback summary.
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
		// No headers found — provide a fallback summary so the agent knows
		// the full content is in the file.
		return fmt.Sprintf("Content has no section headers. Full content (%d chars, %d lines) saved to file.\n",
			len(content), len(lines))
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
