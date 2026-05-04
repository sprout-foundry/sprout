package webui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// SemanticSearchResult represents a single semantic search match.
type SemanticSearchResult struct {
	File       string  `json:"file"`
	Name       string  `json:"name"`       // function/method name
	Signature  string  `json:"signature"`  // full function signature
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Language   string  `json:"language"`
	Similarity float32 `json:"similarity"`
}

// SemanticSearchResponse is the JSON response for semantic search.
type SemanticSearchResponse struct {
	Results  []SemanticSearchResult `json:"results"`
	Query    string                 `json:"query"`
	Total    int                    `json:"total"`
	Duration string                 `json:"duration"` // human-readable elapsed time
}

// handleAPISemanticSearch handles GET /api/search/semantic
func (ws *ReactWebServer) handleAPISemanticSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query parameter is required"})
		return
	}

	topK := 10
	if k := r.URL.Query().Get("top_k"); k != "" {
		if v, err := strconv.Atoi(k); err == nil && v > 0 && v <= 50 {
			topK = v
		}
	}

	threshold := float32(0.75)
	if t := r.URL.Query().Get("threshold"); t != "" {
		if v, err := strconv.ParseFloat(t, 32); err == nil {
			th := float32(v)
			if th >= 0 && th <= 1 {
				threshold = th
			}
		}
	}

	// Resolve client ID using the standard resolution pattern.
	clientID := ws.resolveClientID(r)

	em := ws.getEmbeddingManager(clientID)
	if em == nil {
		writeJSON(w, http.StatusOK, SemanticSearchResponse{
			Results:  []SemanticSearchResult{},
			Query:    query,
			Total:    0,
			Duration: "0ms",
		})
		return
	}

	start := time.Now()
	matches, err := em.QuerySimilar(r.Context(), query, topK, threshold)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Semantic search failed: %v", err)})
		return
	}
	duration := time.Since(start)

	results := make([]SemanticSearchResult, len(matches))
	for i, m := range matches {
		results[i] = SemanticSearchResult{
			File:       m.Record.File,
			Name:       m.Record.Name,
			Signature:  m.Record.Signature,
			StartLine:  m.Record.StartLine,
			EndLine:    m.Record.EndLine,
			Language:   m.Record.Language,
			Similarity: m.Similarity,
		}
	}

	writeJSON(w, http.StatusOK, SemanticSearchResponse{
		Results:  results,
		Query:    query,
		Total:    len(results),
		Duration: duration.String(),
	})
}

// EmbeddingIndexStatus represents the current state of the embedding index.
type EmbeddingIndexStatus struct {
	Available   bool   `json:"available"`   // whether embedding manager exists
	Initialized bool   `json:"initialized"` // whether ONNX provider is initialized
	RecordCount int    `json:"record_count"` // number of indexed code units
	Workspace   string `json:"workspace"`   // workspace root path
}

// handleAPISemanticStatus handles GET /api/search/semantic/status
func (ws *ReactWebServer) handleAPISemanticStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	em := ws.getEmbeddingManager(clientID)

	if em == nil {
		writeJSON(w, http.StatusOK, EmbeddingIndexStatus{
			Available:   false,
			Initialized: false,
			RecordCount: 0,
			Workspace:   ws.GetWorkspaceRoot(),
		})
		return
	}

	writeJSON(w, http.StatusOK, EmbeddingIndexStatus{
		Available:   true,
		Initialized: em.IsInitialized(),
		RecordCount: em.IndexSize(),
		Workspace:   ws.GetWorkspaceRoot(),
	})
}

// getEmbeddingManager returns the embedding manager for the given client's agent.
// Returns nil if the client has no active agent or no embedding manager configured.
func (ws *ReactWebServer) getEmbeddingManager(clientID string) *embedding.EmbeddingManager {
	if clientID == "" {
		return nil
	}
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	if ctx == nil || ctx.Agent == nil {
		return nil
	}
	return ctx.Agent.GetEmbeddingManager()
}

// SnippetLine represents a single line in a code snippet preview.
type SnippetLine struct {
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
	IsContext  bool   `json:"is_context"` // true for lines before the function start
}

// SemanticPreviewResponse is the JSON response for semantic preview.
type SemanticPreviewResponse struct {
	File        string       `json:"file"`
	StartLine   int          `json:"start_line"`
	Snippet     []SnippetLine `json:"snippet"`
	TotalLines  int          `json:"total_lines"`
}

// handleAPISemanticPreview handles GET /api/search/semantic/preview
// Returns a code snippet for the given file and line range.
// Query params: file (required), start_line (required), context (optional, default 8)
func (ws *ReactWebServer) handleAPISemanticPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file parameter is required"})
		return
	}

	startLine := 0
	if sl := r.URL.Query().Get("start_line"); sl != "" {
		if v, err := strconv.Atoi(sl); err == nil && v > 0 {
			startLine = v
		}
	}
	if startLine == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_line parameter is required"})
		return
	}

	contextLines := 8
	if cl := r.URL.Query().Get("context"); cl != "" {
		if v, err := strconv.Atoi(cl); err == nil && v > 0 && v <= 30 {
			contextLines = v
		}
	}

	// Resolve the file path relative to workspace root
	workspaceRoot := ws.GetWorkspaceRoot()
	absPath := filepath.Join(workspaceRoot, filePath)

	// Security: ensure the path is within the workspace
	if !strings.HasPrefix(filepath.Clean(absPath), filepath.Clean(workspaceRoot)) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "path outside workspace"})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	lines := strings.Split(string(data), "\n")

	// Calculate snippet range (start_line - 2 for context before, start_line + contextLines for after)
	snippetStart := startLine - 2
	if snippetStart < 1 {
		snippetStart = 1
	}
	snippetEnd := startLine + contextLines
	if snippetEnd > len(lines) {
		snippetEnd = len(lines)
	}

	// Build snippet lines with line numbers
	var snippet []SnippetLine
	for i := snippetStart; i <= snippetEnd; i++ {
		content := ""
		if i-1 < len(lines) {
			content = lines[i-1]
		}
		snippet = append(snippet, SnippetLine{
			LineNumber: i,
			Content:    content,
			IsContext:  i < startLine,
		})
	}

	writeJSON(w, http.StatusOK, SemanticPreviewResponse{
		File:       filePath,
		StartLine:  startLine,
		Snippet:    snippet,
		TotalLines: len(lines),
	})
}
