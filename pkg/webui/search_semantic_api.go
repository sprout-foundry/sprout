package webui

import (
	"context"
	"fmt"
	"log"
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
	Type       string  `json:"type"`       // "code_unit" or "file"
}

// DuplicateCluster represents a group of files that have highly similar code units.
type DuplicateCluster struct {
	Files      []string `json:"files"`
	Similarity float32  `json:"similarity"`
}

// SemanticSearchResponse is the JSON response for semantic search.
type SemanticSearchResponse struct {
	Results           []SemanticSearchResult `json:"results"`
	Query             string                 `json:"query"`
	Total             int                    `json:"total"`
	Duration          string                 `json:"duration"` // human-readable elapsed time
	DuplicateClusters []DuplicateCluster     `json:"duplicate_clusters"`
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
			Results:           []SemanticSearchResult{},
			Query:             query,
			Total:             0,
			Duration:          "0ms",
			DuplicateClusters: []DuplicateCluster{},
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
			Type:       m.Record.Type,
		}
	}

	// Detect duplicate clusters from the results
	duplicateClusters := detectDuplicateClusters(results)

	writeJSON(w, http.StatusOK, SemanticSearchResponse{
		Results:           results,
		Query:             query,
		Total:             len(results),
		Duration:          duration.String(),
		DuplicateClusters: duplicateClusters,
	})
}

// EmbeddingIndexStatus represents the current state of the embedding index.
type EmbeddingIndexStatus struct {
	Available    bool   `json:"available"`     // whether embedding manager exists
	Initialized  bool   `json:"initialized"`   // whether ONNX provider is initialized
	Building     bool   `json:"building"`      // whether an index build is in progress
	RecordCount  int    `json:"record_count"`  // number of indexed code units
	Workspace    string `json:"workspace"`     // workspace root path
	InitError    string `json:"init_error,omitempty"`  // error from failed initialization, if any
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
		// No active agent session — check if embedding is enabled in config
		// so the frontend can show "available" even when browsing without
		// an active agent.
		embeddingEnabled := false
		if cm := ws.resolveConfigManagerQuietly(r); cm != nil {
			cfg := cm.GetConfig()
			if cfg != nil {
				ei := cfg.EmbeddingIndex
				if ei == nil {
					// Nil means defaults apply: embedding is enabled by default
					embeddingEnabled = true
				} else {
					embeddingEnabled = ei.Enabled
				}
			}
		}
		if embeddingEnabled {
			writeJSON(w, http.StatusOK, EmbeddingIndexStatus{
				Available:   true,
				Initialized: false,
				Building:    false,
				RecordCount: 0,
				Workspace:   ws.GetWorkspaceRoot(),
			})
		} else {
			writeJSON(w, http.StatusOK, EmbeddingIndexStatus{
				Available:   false,
				Initialized: false,
				Building:    false,
				RecordCount: 0,
				Workspace:   ws.GetWorkspaceRoot(),
			})
		}
		return
	}

	writeJSON(w, http.StatusOK, EmbeddingIndexStatus{
		Available:    true,
		Initialized:  em.IsInitialized(),
		Building:     em.IsBuilding(),
		RecordCount:  em.IndexSize(),
		Workspace:    ws.GetWorkspaceRoot(),
		InitError:    initErrorMessage(em.InitError()),
	})
}

// initErrorMessage converts an init error to a user-friendly message,
// returning empty string if no error.
func initErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// handleAPISemanticBuild handles POST /api/search/semantic/build
// Triggers a full index build. Returns immediately with status while building in background.
func (ws *ReactWebServer) handleAPISemanticBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	em := ws.getEmbeddingManager(clientID)
	if em == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "embedding manager not available"})
		return
	}

	if em.IsBuilding() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "build already in progress"})
		return
	}

	// Start build in background goroutine
	go func() {
		ctx := context.Background()
		stats, err := em.BuildIndex(ctx)
		if err != nil {
			log.Printf("[embedding] background build failed: %v", err)
			return
		}
		log.Printf("[embedding] background build complete: %d units indexed", stats.UnitsExtracted)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "build started"})
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

// detectDuplicateClusters detects groups of files that have highly similar code units.
// It builds clusters from results where different files have 2+ code_unit results with similarity >= 0.85.
func detectDuplicateClusters(results []SemanticSearchResult) []DuplicateCluster {
	// Build a map from file path to list of code_unit results from that file
	fileResults := make(map[string][]SemanticSearchResult)
	for _, result := range results {
		// Skip file-level records - only cluster code_unit results
		if result.Type == "file" {
			continue
		}
		fileResults[result.File] = append(fileResults[result.File], result)
	}

	var clusters []DuplicateCluster

	// For each pair of files that both have 2+ results with high similarity
	files := make([]string, 0, len(fileResults))
	for file := range fileResults {
		files = append(files, file)
	}

	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			file1 := files[i]
			file2 := files[j]
			results1 := fileResults[file1]
			results2 := fileResults[file2]

			// Check if both files have 2+ results
			if len(results1) < 2 || len(results2) < 2 {
				continue
			}

			// Find the max similarity between any pair of results from different files
			var maxSimilarity float32
			hasHighSimilarity := false

			for _, r1 := range results1 {
				for _, r2 := range results2 {
					if r1.Similarity >= 0.85 && r2.Similarity >= 0.85 {
						// Both results have high similarity to the query
						if r1.Similarity+r2.Similarity > maxSimilarity {
							maxSimilarity = r1.Similarity + r2.Similarity
						}
						hasHighSimilarity = true
					}
				}
			}

			if hasHighSimilarity {
				// Normalize to average similarity for the cluster
				avgSimilarity := maxSimilarity / 2
				clusters = append(clusters, DuplicateCluster{
					Files:      []string{file1, file2},
					Similarity: avgSimilarity,
				})
			}
		}
	}

	// Sort clusters by similarity (highest first) and limit to top 5
	if len(clusters) > 5 {
		// Simple bubble sort for stability with small slices
		for i := 0; i < len(clusters)-1; i++ {
			for j := 0; j < len(clusters)-i-1; j++ {
				if clusters[j].Similarity < clusters[j+1].Similarity {
					clusters[j], clusters[j+1] = clusters[j+1], clusters[j]
				}
			}
		}
		clusters = clusters[:5]
	}

	return clusters
}
