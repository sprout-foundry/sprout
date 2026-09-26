//go:build !js

package webui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// SemanticSearchResult represents a single semantic search match.
type SemanticSearchResult struct {
	File       string    `json:"file"`
	Name       string    `json:"name"`      // function/method name
	Signature  string    `json:"signature"` // full function signature
	StartLine  int       `json:"start_line"`
	EndLine    int       `json:"end_line"`
	Language   string    `json:"language"`
	Similarity float32   `json:"similarity"`
	Type       string    `json:"type"`                 // "code_unit" or "file"
	Embedding  []float32 `json:"-"`                    // used only for server-side pairwise comparison; not sent to client
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
	if !requireMethod(w, r, http.MethodGet) {
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

	threshold := float32(embedding.DefaultCodeModelSemanticSearchThreshold)
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
	Available   bool   `json:"available"`            // whether embedding manager exists
	Initialized bool   `json:"initialized"`          // whether embedding provider is initialized
	Building    bool   `json:"building"`             // whether an index build is in progress
	RecordCount int    `json:"record_count"`         // number of indexed code units
	Workspace   string `json:"workspace"`            // workspace root path
	InitError   string `json:"init_error,omitempty"` // error from failed initialization, if any
}

// handleAPISemanticStatus handles GET /api/search/semantic/status
func (ws *ReactWebServer) handleAPISemanticStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
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
				// Unset means off, matching the tool call sites
				// (tool_duplicates, tool_handlers_file). This previously
				// reported an absent config as enabled, so the panel claimed
				// the index was available on installs that had never opted in.
				embeddingEnabled = cfg.EmbeddingIndex.IsEnabled()
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
		Available:   true,
		Initialized: em.IsInitialized(),
		Building:    em.IsBuilding(),
		RecordCount: em.IndexSize(),
		Workspace:   ws.GetWorkspaceRoot(),
		InitError:   initErrorMessage(em.InitError()),
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
	if !requireMethod(w, r, http.MethodPost) {
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
	utils.SafeGo(ws.log(), "background embedding build", func() {
		ctx := context.Background()
		stats, err := em.BuildIndex(ctx)
		if err != nil {
			ws.log().Error("background embedding build failed", slog.Any("err", err))
			return
		}
		ws.log().Info("background embedding build completed", slog.Int("units_indexed", stats.UnitsExtracted))
	})

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
