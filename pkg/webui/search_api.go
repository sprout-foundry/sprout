package webui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/filediscovery"
	ignore "github.com/sabhiram/go-gitignore"
)

const (
	maxSearchBodyBytes = 10 << 20 // 10 MiB
	maxSearchResults   = 5000     // Hard cap on total matches
	searchTimeout      = 30 * time.Second
)

// SearchResult represents matches in a single file
type SearchResult struct {
	File       string        `json:"file"`
	Matches    []SearchMatch `json:"matches"`
	MatchCount int           `json:"match_count"`
}

// SearchMatch represents a single match within a file
type SearchMatch struct {
	LineNumber    int      `json:"line_number"`
	Line          string   `json:"line"`
	ColumnStart   int      `json:"column_start"`
	ColumnEnd     int      `json:"column_end"`
	ContextBefore []string `json:"context_before"`
	ContextAfter  []string `json:"context_after"`
}

// SearchResponse represents the response from a search
type SearchResponse struct {
	Results      []SearchResult `json:"results"`
	TotalMatches int            `json:"total_matches"`
	TotalFiles   int            `json:"total_files"`
	Truncated    bool           `json:"truncated"`
	Query        string         `json:"query"`
}

// ReplaceRequest represents a search and replace operation
type ReplaceRequest struct {
	Search        string   `json:"search"`
	Replace       string   `json:"replace"`
	Files         []string `json:"files"`
	CaseSensitive bool     `json:"case_sensitive"`
	WholeWord     bool     `json:"whole_word"`
	Regex         bool     `json:"regex"`
	Preview       bool     `json:"preview"`
}

// ReplaceMatch represents a match that would be replaced
type ReplaceMatch struct {
	LineNumber  int    `json:"line_number"`
	OldLine     string `json:"old_line"`
	NewLine     string `json:"new_line"`
	ColumnStart int    `json:"column_start"`
	ColumnEnd   int    `json:"column_end"`
}

// ReplaceFileChange represents changes to a single file
type ReplaceFileChange struct {
	File         string         `json:"file"`
	Matches      []ReplaceMatch `json:"matches"`
	ChangedLines int            `json:"changed_lines"`
}

// ReplaceResponse represents the response from a replace operation
type ReplaceResponse struct {
	Changes      []ReplaceFileChange `json:"changes"`
	TotalChanges int                 `json:"total_changes"`
	Preview      bool                `json:"preview"`
}

// Search pattern length limit (sanity check for regex DoS prevention)
const maxPatternLength = 1024

// handleAPIQuerySearch handles GET /api/search endpoint
func (ws *ReactWebServer) handleAPIQuerySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	query := r.URL.Query().Get("query")
	if query == "" {
		http.Error(w, "Query parameter is required", http.StatusBadRequest)
		return
	}
	if len(query) > maxPatternLength {
		http.Error(w, "Query too long", http.StatusBadRequest)
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()

	caseSensitive := strings.ToLower(r.URL.Query().Get("case_sensitive")) == "true"
	wholeWord := strings.ToLower(r.URL.Query().Get("whole_word")) == "true"
	isRegex := strings.ToLower(r.URL.Query().Get("regex")) == "true"

	maxResults := 500
	if mr := r.URL.Query().Get("max_results"); mr != "" {
		var parsed int
		if _, err := fmt.Sscanf(mr, "%d", &parsed); err == nil && parsed > 0 {
			maxResults = parsed
		}
	}
	if maxResults > maxSearchResults {
		maxResults = maxSearchResults
	}

	contextLines := 2
	if cl := r.URL.Query().Get("context_lines"); cl != "" {
		var parsed int
		if _, err := fmt.Sscanf(cl, "%d", &parsed); err == nil && parsed >= 0 {
			contextLines = parsed
		}
	}

	include := r.URL.Query().Get("include")
	exclude := r.URL.Query().Get("exclude")

	// Get ignore rules
	ignoreRules := filediscovery.GetIgnoreRules(workspaceRoot)

	// Build include/exclude patterns
	includePatterns := parsePatterns(include)
	excludePatterns := parsePatterns(exclude)

	// Perform search
	results, totalMatches, totalFiles, truncated, err := ws.performSearch(ctx, workspaceRoot, query, caseSensitive, wholeWord, isRegex,
		includePatterns, excludePatterns, maxResults, contextLines, ignoreRules)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			http.Error(w, "Search timed out", http.StatusRequestTimeout)
			return
		}
		log.Printf("handleAPIQuerySearch: search error: %v", err)
		http.Error(w, fmt.Sprintf("Search failed: %v", err), http.StatusInternalServerError)
		return
	}

	response := SearchResponse{
		Results:      results,
		TotalMatches: totalMatches,
		TotalFiles:   totalFiles,
		Truncated:    truncated,
		Query:        query,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// performSearch performs the actual search across workspace files
func (ws *ReactWebServer) performSearch(ctx context.Context, workspaceRoot, query string, caseSensitive, wholeWord, isRegex bool,
	includePatterns, excludePatterns []string, maxResults, contextLines int, ignoreRules *ignore.GitIgnore) (
	[]SearchResult, int, int, bool, error) {

	var results []SearchResult
	totalMatches := 0
	totalFiles := 0
	truncated := false

	// Compile the search pattern
	pattern, err := compileSearchPattern(query, caseSensitive, wholeWord, isRegex)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("invalid search pattern: %w", err)
	}

	// Walk the workspace directory
	err = filepath.WalkDir(workspaceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}

		// Check context deadline
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip directories
		if d.IsDir() {
			// Always skip certain directories
			base := filepath.Base(path)
			if base == ".git" || base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}

			// Check gitignore rules
			if ignoreRules != nil {
				relPath, _ := filepath.Rel(workspaceRoot, path)
				if ignoreRules.MatchesPath(relPath) {
					return filepath.SkipDir
				}
			}

			return nil
		}

		// Skip binary files
		if isBinaryFile(path) {
			return nil
		}

		// Check include patterns
		if len(includePatterns) > 0 && !matchesAnyPattern(path, includePatterns) {
			return nil
		}

		// Check exclude patterns
		if matchesAnyPattern(path, excludePatterns) {
			return nil
		}

		// Check gitignore for files
		if ignoreRules != nil {
			relPath, _ := filepath.Rel(workspaceRoot, path)
			if ignoreRules.MatchesPath(relPath) {
				return nil
			}
		}

		// Search in this file
		fileResults, matchCount, err := ws.searchFile(path, pattern, contextLines)
		if err != nil {
			log.Printf("Error searching file %s: %v", path, err)
			return nil
		}

		if matchCount > 0 {
			relPath, _ := filepath.Rel(workspaceRoot, path)
			results = append(results, SearchResult{
				File:       relPath,
				Matches:    fileResults,
				MatchCount: matchCount,
			})
			totalMatches += matchCount
			totalFiles++

			// Check if we've hit the limit
			if totalMatches >= maxResults {
				truncated = true
				return filepath.SkipAll
			}
		}

		return nil
	})

	if err != nil && err != context.DeadlineExceeded {
		return nil, 0, 0, false, fmt.Errorf("search files: %w", err)
	}

	// Sort results by file path for consistent ordering
	sort.Slice(results, func(i, j int) bool {
		return results[i].File < results[j].File
	})

	return results, totalMatches, totalFiles, truncated, nil
}

// searchFile searches for matches in a single file
func (ws *ReactWebServer) searchFile(path string, pattern *regexp.Regexp, contextLines int) ([]SearchMatch, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open file %q: %w", path, err)
	}
	defer file.Close()

	var matches []SearchMatch
	lineNumber := 0
	var lineBuffer []string // Circular buffer of size 2*contextLines+1

	scanner := bufio.NewScanner(file)
	bufferSize := contextLines*2 + 1
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Maintain circular buffer
		if len(lineBuffer) >= bufferSize {
			lineBuffer = lineBuffer[1:]
		}
		lineBuffer = append(lineBuffer, line)

		// Find match (single scan instead of MatchString + FindStringSubmatchIndex)
		loc := pattern.FindStringIndex(line)
		if loc == nil {
			continue
		}

		matches = append(matches, SearchMatch{
			LineNumber:    lineNumber,
			Line:          line,
			ColumnStart:   loc[0] + 1, // 1-based
			ColumnEnd:     loc[1] + 1, // 1-based
			ContextBefore: getContextLines(lineBuffer, len(lineBuffer), contextLines, true),
			ContextAfter:  []string{}, // Populated below
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan file %q: %w", path, err)
	}

	// If context is requested, do a second pass to collect after-context
	if contextLines > 0 && len(matches) > 0 {
		file.Seek(0, 0)
		scanner = bufio.NewScanner(file)
		lineNumber = 0
		for scanner.Scan() {
			lineNumber++
			for _, match := range matches {
				if lineNumber > match.LineNumber && lineNumber <= match.LineNumber+contextLines {
					match.ContextAfter = append(match.ContextAfter, scanner.Text())
				}
			}
		}
	}

	return matches, len(matches), nil
}

// getContextLines extracts context lines from a line buffer
func getContextLines(buffer []string, bufferLen, contextLines int, before bool) []string {
	if contextLines <= 0 {
		return nil
	}
	count := contextLines
	start := bufferLen - 1 - count // -1 to exclude the current match line
	if start < 0 {
		start = 0
	}
	return append([]string{}, buffer[start:bufferLen-1]...)
}

// handleAPIQuerySearchReplace handles POST /api/search/replace endpoint
func (ws *ReactWebServer) handleAPIQuerySearchReplace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxSearchBodyBytes)

	var req ReplaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Search == "" {
		http.Error(w, "Search parameter is required", http.StatusBadRequest)
		return
	}
	if len(req.Search) > maxPatternLength {
		http.Error(w, "Search pattern too long", http.StatusBadRequest)
		return
	}
	if len(req.Replace) > 10000 {
		http.Error(w, "Replace string too long", http.StatusBadRequest)
		return
	}

	// Validate files are within workspace
	for _, file := range req.Files {
		canonicalPath, err := canonicalizePath(file, workspaceRoot, false)
		if err != nil || !isWithinWorkspace(canonicalPath, workspaceRoot) {
			http.Error(w, fmt.Sprintf("File outside workspace: %s", file), http.StatusBadRequest)
			return
		}
	}

	// Compile the search pattern
	pattern, err := compileSearchPattern(req.Search, req.CaseSensitive, req.WholeWord, req.Regex)
	if err != nil {
		http.Error(w, "Invalid search pattern: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Perform replace
	changes, err := ws.performReplace(ws.resolveClientID(r), workspaceRoot, req, pattern)
	if err != nil {
		log.Printf("handleAPIQuerySearchReplace: replace error: %v", err)
		http.Error(w, fmt.Sprintf("Replace failed: %v", err), http.StatusInternalServerError)
		return
	}

	response := ReplaceResponse{
		Changes:      changes,
		TotalChanges: len(changes),
		Preview:      req.Preview,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// performReplace performs the search and replace operation
func (ws *ReactWebServer) performReplace(clientID, workspaceRoot string, req ReplaceRequest, pattern *regexp.Regexp) ([]ReplaceFileChange, error) {
	var changes []ReplaceFileChange

	for _, filePath := range req.Files {
		// Resolve relative path against workspace root
		absFilePath := filePath
		if !filepath.IsAbs(filePath) {
			absFilePath = filepath.Join(workspaceRoot, filePath)
		}

		// Open file
		content, err := os.ReadFile(absFilePath)
		if err != nil {
			log.Printf("Error reading file %s: %v", absFilePath, err)
			continue
		}

		lines := strings.Split(string(content), "\n")
		var fileChanges []ReplaceMatch
		var newLines []string

		// Process each line
		for i, line := range lines {
			newLine := line
			matches := pattern.FindAllStringSubmatchIndex(line, -1)

			if len(matches) > 0 {
				// Apply replacements from end to start to maintain indices
				for j := len(matches) - 1; j >= 0; j-- {
					match := matches[j]
					columnStart := match[2]
					columnEnd := match[3]

					// Create ReplaceMatch for preview
					fileChanges = append(fileChanges, ReplaceMatch{
						LineNumber:  i + 1,
						OldLine:     line,
						NewLine:     line[:columnStart] + req.Replace + line[columnEnd:],
						ColumnStart: columnStart + 1, // Convert to 1-based
						ColumnEnd:   columnEnd + 1,   // Convert to 1-based
					})

					// Apply replacement
					newLine = line[:columnStart] + req.Replace + line[columnEnd:]
				}
			}

			newLines = append(newLines, newLine)
		}

		if len(fileChanges) > 0 {
			change := ReplaceFileChange{
				File:         filePath,
				Matches:      fileChanges,
				ChangedLines: len(fileChanges),
			}

			if !req.Preview {
				// Write changes to file
				newContent := strings.Join(newLines, "\n")
				if err := os.WriteFile(absFilePath, []byte(newContent), 0644); err != nil {
					log.Printf("Error writing file %s: %v", absFilePath, err)
					continue
				}

				// Publish file change event
				ws.publishClientEvent(clientID, events.EventTypeFileChanged, events.FileChangedEvent(absFilePath, "write", newContent))
			}

			changes = append(changes, change)
		}
	}

	return changes, nil
}

// compileSearchPattern compiles a search pattern with optional modifiers
func compileSearchPattern(query string, caseSensitive, wholeWord, isRegex bool) (*regexp.Regexp, error) {
	pattern := query

	if isRegex {
		if !caseSensitive {
			pattern = "(?i)" + pattern
		}
		if wholeWord {
			pattern = `(?m)\b` + pattern + `\b`
		}
		return regexp.Compile(pattern)
	}

	// Escape special regex characters for plain text search
	escaped := regexp.QuoteMeta(pattern)

	if !caseSensitive {
		escaped = "(?i)" + escaped
	}

	if wholeWord {
		escaped = `(?m)\b` + escaped + `\b`
	}

	return regexp.Compile(escaped)
}

// isBinaryFile checks if a file appears to be binary
func isBinaryFile(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	// Read first 8KB
	buffer := make([]byte, 8192)
	n, err := io.ReadFull(file, buffer)
	if err != nil && err != io.ErrUnexpectedEOF {
		return false
	}

	// Check for null bytes
	for i := 0; i < n; i++ {
		if buffer[i] == 0 {
			return true
		}
	}

	return false
}

// parsePatterns parses a comma-separated pattern string into a slice
func parsePatterns(patterns string) []string {
	if patterns == "" {
		return nil
	}

	var result []string
	parts := strings.Split(patterns, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	return result
}

// matchesAnyPattern checks if a path matches any of the patterns
func matchesAnyPattern(path string, patterns []string) bool {
	base := filepath.Base(path)
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if matched, err := filepath.Match(pattern, base); err == nil && matched {
			return true
		}
		if matched, err := filepath.Match(pattern, path); err == nil && matched {
			return true
		}
	}
	return false
}
