package webui

import (
	"fmt"
	"net/http"
	"strconv"
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
