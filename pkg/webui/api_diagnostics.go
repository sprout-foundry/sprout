package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alantheprice/ledit/pkg/validation"
)

// diagnosticsRequest is the JSON body for POST /api/diagnostics.
type diagnosticsRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// frontendDiagnostic is the JSON representation sent to the frontend,
// using byte-offset positions compatible with Monaco editors.
type frontendDiagnostic struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// diagnosticsResponse is the JSON response for POST /api/diagnostics.
type diagnosticsResponse struct {
	Message      string               `json:"message"`
	Path         string               `json:"path"`
	Diagnostics  []frontendDiagnostic `json:"diagnostics"`
	Version      string               `json:"version"`
}

// handleAPIDiagnostics handles POST /api/diagnostics.
// It validates Go source content and returns diagnostics for the frontend.
func (ws *ReactWebServer) handleAPIDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)

	var req diagnosticsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}

	// Resolve relative path against workspace root.
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	canonical, err := canonicalizePath(req.Path, workspaceRoot, true)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if !isWithinWorkspace(canonical, workspaceRoot) {
		http.Error(w, "Path is outside workspace", http.StatusForbidden)
		return
	}
	filePath := canonical

	// Gracefully handle missing agent or validator.
	if ws.agent == nil {
		ws.writeDiagnosticsResponse(w, req.Path, nil)
		return
	}

	validator := ws.agent.GetValidator()
	if validator == nil {
		ws.writeDiagnosticsResponse(w, req.Path, nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := validator.RunValidation(ctx, filePath, req.Content)

	frontendDiags := make([]frontendDiagnostic, 0, len(result.Diagnostics))
	for _, d := range result.Diagnostics {
		fe := validationToFrontend(d, req.Content)
		frontendDiags = append(frontendDiags, fe)
	}

	ws.writeDiagnosticsResponse(w, req.Path, frontendDiags)
}

// writeDiagnosticsResponse writes a diagnostics API response.
func (ws *ReactWebServer) writeDiagnosticsResponse(w http.ResponseWriter, path string, diags []frontendDiagnostic) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(diagnosticsResponse{
		Message:     "ok",
		Path:        path,
		Diagnostics: diags,
		Version:     time.Now().Format(time.RFC3339Nano),
	})
}

// validationToFrontend converts a validation.Diagnostic to a frontend-friendly
// format with byte-offset from/to positions.
func validationToFrontend(d validation.Diagnostic, content string) frontendDiagnostic {
	from, to := diagnosticToOffsets(d, content)

	return frontendDiagnostic{
		From:     from,
		To:       to,
		Severity: d.Severity,
		Message:  d.Message,
		Source:   d.Source,
	}
}

// diagnosticToOffsets computes byte-offset from/to for a validation.Diagnostic.
//
//	- For syntax errors (source = "gofmt"), the error message typically contains
//	  line/column info like "<standard input>:42:5: expected ...". We parse
//	  the line and column, then convert to byte offsets.
//	- For import issues where line and column are both 1, we set from=0,
//	  to=len(content) to span the entire file (the import system doesn't
//	  provide specific locations).
//	- For diagnostics with valid line/column > 1, convert directly.
func diagnosticToOffsets(d validation.Diagnostic, content string) (int, int) {
	// Import issues: span the entire file.
	if d.Line == 1 && d.Column == 1 && d.Source == "goimports" {
		return 0, len(content)
	}

	// Syntax errors: try to parse line/column from the error message.
	if d.Source == "gofmt" {
		line, col, parsed := parseGofmtError(d.Message)
		if parsed && line > 0 {
			return lineColToOffsets(line, col, content)
		}
	}

	// Fallback: use the diagnostic's line/column directly.
	if d.Line > 0 {
		return lineColToOffsets(d.Line, d.Column, content)
	}

	// Last resort: span entire content.
	return 0, len(content)
}

// parseGofmtError extracts line and column from a gofmt error message.
// gofmt errors typically look like:
//
//	<standard input>:42:5: expected declaration, found 'fmt'
//	<stdin>:10:2: expected 'package'
func parseGofmtError(msg string) (line, col int, ok bool) {
	// Strip the "syntax error: " prefix that ValidateSyntax adds.
	msg = strings.TrimPrefix(msg, "syntax error: ")

	// Find the colon-separated segments after the file reference.
	// Format: <file>:<line>:<col>: <message>
	// The file reference can be "<standard input>" or "<stdin>".
	idx := strings.Index(msg, ":")
	if idx < 0 {
		return 0, 0, false
	}
	// Skip past the file reference.
	rest := msg[idx+1:]

	// Parse line number.
	lineEnd := strings.Index(rest, ":")
	if lineEnd < 0 {
		return 0, 0, false
	}
	lineStr := rest[:lineEnd]
	l, err := strconv.Atoi(lineStr)
	if err != nil {
		return 0, 0, false
	}

	// Parse column number.
	rest = rest[lineEnd+1:]
	colEnd := strings.Index(rest, ":")
	if colEnd < 0 {
		return 0, 0, false
	}
	colStr := rest[:colEnd]
	c, err := strconv.Atoi(colStr)
	if err != nil {
		return 0, 0, false
	}

	return l, c, true
}

// lineColToOffsets converts a 1-based line and column to byte offsets in content.
// If the position is beyond the content, the offset is clamped to the content length.
func lineColToOffsets(line, col int, content string) (from, to int) {
	lines := strings.Split(content, "\n")

	// Validate line.
	if line < 1 {
		line = 1
	}
	if line > len(lines) {
		return len(content), len(content)
	}

	// Compute byte offset of the start of the target line.
	offset := 0
	for i := 0; i < line-1; i++ {
		offset += len(lines[i]) + 1 // +1 for newline
	}

	// Compute column offset within the line.
	if col < 1 {
		col = 1
	}
	lineContent := lines[line-1]
	if col > len(lineContent)+1 {
		from = offset + len(lineContent)
	} else {
		from = offset + col - 1
	}

	// If column is beyond the first character, extend 'to' to cover a
	// reasonable token. We take the rest of the word/identifier at from.
	to = extendToTokenEnd(content, from)

	if to < from {
		to = from + 1
	}
	if to > len(content) {
		to = len(content)
	}
	if from > len(content) {
		from = len(content)
	}

	return from, to
}

// extendToTokenEnd extends a byte offset to the end of the current word/token.
// It operates on bytes and only falls back to rune iteration when non-ASCII
// characters are encountered, avoiding the cost of converting the entire content
// to []rune for files that are pure ASCII (the common case for Go source).
func extendToTokenEnd(content string, byteOffset int) int {
	if byteOffset >= len(content) {
		return byteOffset
	}
	if byteOffset < 0 {
		byteOffset = 0
	}

	// Fast path: scan bytes. If any non-ASCII byte is found, switch to
	// rune-based scanning to avoid splitting multi-byte UTF-8 sequences.
	end := byteOffset
	needsScan := false
	for end < len(content) {
		b := content[end]
		if b < utf8.RuneSelf {
			// ASCII — check if it's a delimiter.
			if isExtDelimiter(rune(b)) {
				break
			}
			end++
		} else {
			needsScan = true
			break
		}
	}
	if !needsScan {
		if end == byteOffset {
			end = byteOffset + 1
		}
		if end > len(content) {
			end = len(content)
		}
		return end
	}

	// Slow path: non-ASCII content, switch to rune iteration.
	runeIdx := utf8.RuneCountInString(content[:byteOffset])
	runes := []rune(content)

	if runeIdx >= len(runes) {
		return len(content)
	}

	runeEnd := runeIdx
	for runeEnd < len(runes) {
		if isExtDelimiter(runes[runeEnd]) {
			break
		}
		runeEnd++
	}

	if runeEnd == runeIdx {
		runeEnd = runeIdx + 1
	}
	if runeEnd > len(runes) {
		runeEnd = len(runes)
	}

	return len(string(runes[:runeEnd]))
}

// isExtDelimiter returns true if the rune is a delimiter that should end token extension.
func isExtDelimiter(ch rune) bool {
	switch ch {
	case ' ', '\t', '\n', '\r',
		'(', ')', '{', '}', '[', ']',
		',', ';', ':', '+', '-', '*', '/',
		'=', '!', '<', '>', '&', '|', '^', '%',
		'"', '\'':
		return true
	default:
		return false
	}
}
