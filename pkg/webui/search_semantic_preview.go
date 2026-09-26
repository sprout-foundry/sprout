//go:build !js

package webui

// search_semantic_preview.go — the semantic-preview endpoint: the
// handleAPISemanticPreview handler, the duplicate-cluster detector
// (detectDuplicateClusters), and the preview response types (SnippetLine,
// SemanticPreviewResponse). The search / status / build endpoints stay in
// search_semantic_api.go; the preview-context endpoint is in
// search_semantic_context.go. Split out of search_semantic_api.go.
import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// SnippetLine represents a single line in a code snippet preview.
type SnippetLine struct {
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
	IsContext  bool   `json:"is_context"` // true for lines before the function start
}

// SemanticPreviewResponse is the JSON response for semantic preview.
type SemanticPreviewResponse struct {
	File       string        `json:"file"`
	StartLine  int           `json:"start_line"`
	Snippet    []SnippetLine `json:"snippet"`
	TotalLines int           `json:"total_lines"`
}

// handleAPISemanticPreview handles GET /api/search/semantic/preview
// Returns a code snippet for the given file and line range.
// Query params: file (required), start_line (required), context (optional, default 8)
func (ws *ReactWebServer) handleAPISemanticPreview(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
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
