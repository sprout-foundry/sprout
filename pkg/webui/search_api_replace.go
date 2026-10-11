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
	"unicode/utf16"

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

	totalChanges := 0
	for _, change := range changes {
		totalChanges += len(change.Matches)
	}
	response := ReplaceResponse{
		Changes:      changes,
		TotalChanges: totalChanges,
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

		// Every non-empty match on a line is replaced by the replacement text
		// as written: the replacement has no backreference syntax, so `$1`,
		// `$&` and `\` stay literal. Each replaced occurrence is one entry,
		// with 1-based UTF-16 columns, the in-browser handler's shape.
		changedLines := 0
		for i, line := range lines {
			spans := nonEmptyMatches(pattern, line)
			if len(spans) == 0 {
				newLines = append(newLines, line)
				continue
			}
			var b strings.Builder
			cursor := 0
			for _, span := range spans {
				b.WriteString(line[cursor:span[0]])
				b.WriteString(req.Replace)
				cursor = span[1]
			}
			b.WriteString(line[cursor:])
			newLine := b.String()
			for _, span := range spans {
				fileChanges = append(fileChanges, ReplaceMatch{
					LineNumber:  i + 1,
					OldLine:     line,
					NewLine:     newLine,
					ColumnStart: utf16Len(line[:span[0]]) + 1,
					ColumnEnd:   utf16Len(line[:span[1]]) + 1,
				})
			}
			if newLine != line {
				changedLines++
			}
			newLines = append(newLines, newLine)
		}

		if len(fileChanges) > 0 {
			change := ReplaceFileChange{
				File:         filePath,
				Matches:      fileChanges,
				ChangedLines: changedLines,
			}

			if !req.Preview && changedLines > 0 {
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

// nonEmptyMatches returns the byte spans of the pattern's non-empty matches
// in line. An empty match (a regex such as `x*`) replaces nothing.
func nonEmptyMatches(pattern *regexp.Regexp, line string) [][2]int {
	var spans [][2]int
	for _, m := range pattern.FindAllStringIndex(line, -1) {
		if m[1] > m[0] {
			spans = append(spans, [2]int{m[0], m[1]})
		}
	}
	return spans
}

// utf16Len is the length of s in UTF-16 code units, the unit browser
// string offsets use.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
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
