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
	File       string    `json:"file"`
	Name       string    `json:"name"`       // function/method name
	Signature  string    `json:"signature"`  // full function signature
	StartLine  int       `json:"start_line"`
	EndLine    int       `json:"end_line"`
	Language   string    `json:"language"`
	Similarity float32   `json:"similarity"`
	Type       string    `json:"type"`       // "code_unit" or "file"
	Embedding  []float32 `json:"-"`  // used only for server-side pairwise comparison; not sent to client
	ClusterId  int       `json:"cluster_id,omitempty"` // 0 = not in a cluster, 1+ = cluster group
}

// DuplicateCluster represents a group of files that have highly similar code units.
type DuplicateCluster struct {
	Files      []string `json:"files"`
	Similarity float32  `json:"similarity"` // average pairwise similarity
	Count      int      `json:"count"`      // number of results in cluster
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
			Embedding:  m.Record.Embedding,
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

// detectDuplicateClusters detects groups of code units that are highly similar to each other.
// It computes actual pairwise cosine similarity between result embeddings.
// Clusters are formed using a greedy union-find approach: if A~B and B~C, they're all in the same cluster.
// Cluster threshold: pairwise cosine similarity >= 0.90
// Only code_unit results from different files are clustered.
//
// NOTE: This function mutates the input results slice by assigning ClusterId fields.
// The caller must ensure the slice is not shared or cached.
func detectDuplicateClusters(results []SemanticSearchResult) []DuplicateCluster {
	const clusterThreshold = float32(0.90)

	// Filter code_unit results and assign indices
	codeUnits := []int{} // indices into results array
	for i := range results {
		if results[i].Type == "code_unit" {
			codeUnits = append(codeUnits, i)
		}
	}

	if len(codeUnits) < 2 {
		return nil
	}

	// Union-Find data structure for clustering
	parent := make([]int, len(codeUnits))
	rank := make([]int, len(codeUnits))
	for i := range parent {
		parent[i] = i
		rank[i] = 0
	}

	// Find with path compression
	var find func(x int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	// Union by rank
	union := func(x, y int) {
		px, py := find(x), find(y)
		if px == py {
			return
		}
		if rank[px] < rank[py] {
			parent[px] = py
		} else if rank[px] > rank[py] {
			parent[py] = px
		} else {
			parent[py] = px
			rank[px]++
		}
	}

	// Compute pairwise similarity and union similar results
	// Track similarities for computing average later
	type pairSimilarity struct {
		a, b       int // indices into codeUnits
		similarity float32
	}
	var similarPairs []pairSimilarity

	for i := 0; i < len(codeUnits); i++ {
		for j := i + 1; j < len(codeUnits); j++ {
			idxI, idxJ := codeUnits[i], codeUnits[j]
			resultI := results[idxI]
			resultJ := results[idxJ]

			// Only compare results from different files
			if resultI.File == resultJ.File {
				continue
			}

			// Compute pairwise cosine similarity between embeddings
			sim := embedding.CosineSimilarity(resultI.Embedding, resultJ.Embedding)
			if sim >= clusterThreshold {
				union(i, j)
				similarPairs = append(similarPairs, pairSimilarity{a: i, b: j, similarity: sim})
			}
		}
	}

	if len(similarPairs) == 0 {
		return nil
	}

	// Group results by cluster
	clusters := make(map[int][]int) // root -> list of codeUnit indices
	for i := range codeUnits {
		root := find(i)
		clusters[root] = append(clusters[root], i)
	}

	// Build duplicate clusters, filtering by size (must have 2+ results from 2+ files)
	var duplicateClusters []DuplicateCluster
	nextClusterId := 1

	for root, members := range clusters {
		if len(members) < 2 {
			continue
		}

		// Check if cluster has results from 2+ different files
		filesMap := make(map[string]bool)
		for _, idx := range members {
			filesMap[results[codeUnits[idx]].File] = true
		}
		if len(filesMap) < 2 {
			continue
		}

		// Compute average similarity for this cluster
		var totalSim float32
		var pairCount int
		for _, pair := range similarPairs {
			if find(pair.a) == root || find(pair.b) == root {
				totalSim += pair.similarity
				pairCount++
			}
		}
		avgSim := float32(0)
		if pairCount > 0 {
			avgSim = totalSim / float32(pairCount)
		}

		// Collect files in this cluster
		files := make([]string, 0, len(filesMap))
		for file := range filesMap {
			files = append(files, file)
		}

		duplicateClusters = append(duplicateClusters, DuplicateCluster{
			Files:      files,
			Similarity: avgSim,
			Count:      len(members),
		})

		// Assign ClusterId to each result in the cluster
		for _, idx := range members {
			results[codeUnits[idx]].ClusterId = nextClusterId
		}
		nextClusterId++
	}

	// Sort clusters by similarity (highest first)
	for i := 0; i < len(duplicateClusters)-1; i++ {
		for j := 0; j < len(duplicateClusters)-i-1; j++ {
			if duplicateClusters[j].Similarity < duplicateClusters[j+1].Similarity {
				duplicateClusters[j], duplicateClusters[j+1] = duplicateClusters[j+1], duplicateClusters[j]
			}
		}
	}

	// Limit to top 5 clusters
	if len(duplicateClusters) > 5 {
		duplicateClusters = duplicateClusters[:5]
	}

	return duplicateClusters
}
