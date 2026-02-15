package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

const (
	defaultSearchMaxResults = 50
	defaultSearchMaxBytes   = 20 * 1024
	defaultSearchLineLength = 240
)

// Tool handler implementations for search operations

// normalizePositiveInt normalizes various numeric types to a positive int
func normalizePositiveInt(value any) int {
	const maxInt = int(^uint(0) >> 1)
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int8:
		if v > 0 {
			return int(v)
		}
	case int16:
		if v > 0 {
			return int(v)
		}
	case int32:
		if v > 0 {
			return int(v)
		}
	case int64:
		if v > 0 && v <= int64(maxInt) {
			return int(v)
		}
	case uint:
		if v64 := uint64(v); v64 > 0 && v64 <= uint64(maxInt) {
			return int(v)
		}
	case uint8:
		if v > 0 {
			return int(v)
		}
	case uint16:
		if v > 0 {
			return int(v)
		}
	case uint32:
		if v64 := uint64(v); v64 > 0 && v64 <= uint64(maxInt) {
			return int(v)
		}
	case uint64:
		if v > 0 && v <= uint64(maxInt) {
			return int(v)
		}
	case float32:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return normalizePositiveInt(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return normalizePositiveInt(i)
		}
	}
	return 0
}

func handleSearchFiles(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	var pattern string
	if p, ok := args["search_pattern"].(string); ok {
		pattern = p
	} else if p, ok := args["pattern"].(string); ok {
		pattern = p
	} else {
		return "", fmt.Errorf("missing required parameter 'search_pattern'")
	}

	root := "."
	if v, ok := args["directory"].(string); ok && strings.TrimSpace(v) != "" {
		root = v
	}

	glob := ""
	if v, ok := args["file_glob"].(string); ok {
		glob = v
	} else if v, ok := args["file_pattern"].(string); ok {
		glob = v
	}

	caseSensitive := false
	if v, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = v
	}

	maxResults := defaultSearchMaxResults
	if v, ok := args["max_results"]; ok {
		if normalized := normalizePositiveInt(v); normalized > 0 {
			maxResults = normalized
		}
	}

	maxBytes := defaultSearchMaxBytes
	if v, ok := args["max_bytes"]; ok {
		if normalized := normalizePositiveInt(v); normalized > 0 {
			maxBytes = normalized
		}
	}

	a.ToolLog("searching files", fmt.Sprintf("pattern=%q in %s", pattern, root))
	a.debugLog("Searching files: pattern=%q, root=%s, max_results=%d\n", pattern, root, maxResults)

	// Prepare matcher: try regex first, then fallback to substring
	var re *regexp.Regexp
	var err error
	if caseSensitive {
		re, err = regexp.Compile(pattern)
	} else {
		re, err = regexp.Compile("(?i)" + pattern)
	}
	useRegex := err == nil

	// Default excluded directories
	excluded := map[string]bool{
		".git":         true,
		"node_modules": true,
		".ledit":       true,
		".venv":        true,
		"dist":         true,
		"build":        true,
		".cache":       true,
	}

	matched := 0
	var b strings.Builder
	searchCapped := false

	// Limit per-file read to avoid huge files (in bytes)
	const maxFileSize = 2 * 1024 * 1024 // 2MB

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if searchCapped {
			return io.EOF
		}
		if err != nil {
			return nil // skip on error
		}
		name := d.Name()
		if d.IsDir() {
			if excluded[name] {
				return filepath.SkipDir
			}
			// Skip hidden dirs unless explicitly included via pattern/glob (keep simple)
			if strings.HasPrefix(name, ".") && !strings.HasPrefix(name, ".env") {
				if name != "." && name != ".." {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Glob filter
		if glob != "" {
			// Use base name for typical patterns
			if ok, _ := filepath.Match(glob, name); !ok {
				return nil
			}
		}

		// Basic binary guard by extension
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".webp",
			".pdf", ".zip", ".tar", ".gz", ".rar", ".7z",
			".mp3", ".wav", ".ogg", ".flac", ".aac",
			".mp4", ".avi", ".mov", ".wmv", ".mkv",
			".exe", ".dll", ".so", ".dylib", ".bin",
			".db", ".sqlite", ".ico", ".woff", ".woff2", ".ttf":
			return nil
		}

		// Open file and scan
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		// Size cap
		if info, err := f.Stat(); err == nil && info.Size() > maxFileSize {
			// Read only first maxFileSize bytes
			r := io.LimitReader(f, maxFileSize)
			buf := make([]byte, maxFileSize)
			n, _ := io.ReadFull(r, buf)
			buf = buf[:n]
			// naive binary check: look for NUL
			if bytesIndexByte(buf, 0) >= 0 {
				return nil
			}
			// search within this chunk by lines
			if searchBufferLines(&b, path, string(buf), re, pattern, caseSensitive, useRegex, &matched, maxResults, maxBytes) {
				searchCapped = true
				return io.EOF // stop walking by returning non-nil? better: track and stop later
			}
			return nil
		}

		content, err := io.ReadAll(f)
		if err != nil {
			return nil
		}
		// binary check
		if bytesIndexByte(content, 0) >= 0 {
			return nil
		}
		if searchBufferLines(&b, path, string(content), re, pattern, caseSensitive, useRegex, &matched, maxResults, maxBytes) {
			searchCapped = true
			return io.EOF
		}
		return nil
	})

	if walkErr != nil && walkErr != io.EOF {
		return "", fmt.Errorf("search failed: %v", walkErr)
	}

	if matched == 0 {
		return fmt.Sprintf("No matches found for pattern '%s' in %s", pattern, root), nil
	}
	return b.String(), nil
}

func handleWebSearch(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for web_search tool")
	}

	query := args["query"].(string)
	a.ToolLog("searching web", fmt.Sprintf("query=%q", truncateString(query, 50)))
	a.debugLog("Performing web search: %s\n", query)

	if a.configManager == nil {
		return "", fmt.Errorf("configuration manager not initialized for web search")
	}

	result, err := tools.WebSearch(query, a.configManager)
	a.debugLog("Web search error: %v\n", err)
	return result, err
}

func handleFetchURL(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for fetch_url tool")
	}

	url := args["url"].(string)
	a.ToolLog("fetching URL", fmt.Sprintf("url=%q", truncateString(url, 50)))
	a.debugLog("Fetching URL: %s\n", url)

	if a.configManager == nil {
		return "", fmt.Errorf("configuration manager not initialized for URL fetch")
	}

	result, err := tools.FetchURL(url, a.configManager)
	a.debugLog("Fetch URL error: %v\n", err)
	return result, err
}

// Helper functions for search handlers

// bytesIndexByte is a small helper to avoid importing bytes for one call
func bytesIndexByte(b []byte, c byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// searchBufferLines scans lines of content and appends matches; returns true if max reached
func searchBufferLines(b *strings.Builder, path, content string, re *regexp.Regexp, pattern string, caseSensitive, useRegex bool, matched *int, max int, maxBytes int) bool {
	// Normalize to forward slashes for readability
	norm := filepath.ToSlash(path)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if maxBytes > 0 && b.Len() >= maxBytes {
			return true
		}
		if *matched >= max {
			return true
		}
		ok := false
		if useRegex {
			ok = re.FindStringIndex(line) != nil
		} else {
			if caseSensitive {
				ok = strings.Contains(line, pattern)
			} else {
				ok = strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
			}
		}
		if ok {
			lineOut := line
			if defaultSearchLineLength > 0 && len(lineOut) > defaultSearchLineLength {
				lineOut = truncateString(lineOut, defaultSearchLineLength)
			}
			// Format similar to grep: path:line:content
			fmt.Fprintf(b, "%s:%d:%s\n", norm, i+1, lineOut)
			*matched++
			if maxBytes > 0 && b.Len() >= maxBytes {
				return true
			}
		}
	}
	return false
}
