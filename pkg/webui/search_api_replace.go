//go:build !js

package webui

// search_api_replace.go — the /api/query/search/replace endpoint:
// the Replace* wire types, handleAPIQuerySearchReplace, the
// performReplace apply path, and the multi-pattern helpers
// (parsePatterns, matchesAnyPattern). Split out of search_api.go.
import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

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

// handleAPIQuerySearchReplace handles POST /api/search/replace endpoint
func (ws *ReactWebServer) handleAPIQuerySearchReplace(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxSearchBodyBytes)

	var req ReplaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON: "+err.Error())
		return
	}

	if req.Search == "" {
		writeJSONErr(w, http.StatusBadRequest, "search_parameter_required", "Search parameter is required")
		return
	}
	if len(req.Search) > maxPatternLength {
		writeJSONErr(w, http.StatusBadRequest, "search_pattern_too_long", "Search pattern too long")
		return
	}
	if len(req.Replace) > 10000 {
		writeJSONErr(w, http.StatusBadRequest, "replace_string_too_long", "Replace string too long")
		return
	}

	// Validate files are within workspace
	for _, file := range req.Files {
		canonicalPath, err := canonicalizePath(file, workspaceRoot, false)
		if err != nil || !isWithinWorkspace(canonicalPath, workspaceRoot) {
			writeJSONErr(w, http.StatusBadRequest, "file_outside_workspace", fmt.Sprintf("File outside workspace: %s", file))
			return
		}
	}

	// Compile the search pattern
	pattern, err := compileSearchPattern(req.Search, req.CaseSensitive, req.WholeWord, req.Regex)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_search_pattern", "Invalid search pattern: "+err.Error())
		return
	}

	// Perform replace
	changes, err := ws.performReplace(ws.resolveClientID(r), workspaceRoot, req, pattern)
	if err != nil {
		ws.log().Error("search replacement failed", slog.Any("err", err))
		writeJSONErr(w, http.StatusInternalServerError, "replace_failed", fmt.Sprintf("Replace failed: %v", err))
		return
	}

	response := ReplaceResponse{
		Changes:      changes,
		TotalChanges: len(changes),
		Preview:      req.Preview,
	}

	writeJSON(w, http.StatusOK, response)
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

		// Open file, skipping files that exceed the read size limit
		if info, statErr := os.Stat(absFilePath); statErr == nil && info.Size() > maxFileReadSize {
			ws.log().Debug("skipping oversized file during replacement", slog.String("path", absFilePath), slog.Int64("size", info.Size()), slog.Int64("max_size", maxFileReadSize))
			continue
		}
		content, err := os.ReadFile(absFilePath)
		if err != nil {
			ws.log().Warn("failed to read file during replacement", slog.String("path", absFilePath), slog.Any("err", err))
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
					ws.log().Error("failed to write replacement file", slog.String("path", absFilePath), slog.Any("err", err))
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
