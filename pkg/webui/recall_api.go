//go:build !js

package webui

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// RecallItem is the JSON shape returned by /api/recall (a friendly subset of agent.RecalledItem).
type RecallItem struct {
	SessionID      string  `json:"session_id"`
	Workspace      string  `json:"workspace"`
	Summary        string  `json:"summary"`
	Actionable     string  `json:"actionable"`
	Similarity     float32 `json:"similarity"`
	AgeDays        float64 `json:"age_days"`
	ContentPreview string  `json:"content_preview"`
}

// RecallResponse is the JSON envelope for the /api/recall endpoint.
type RecallResponse struct {
	Query string       `json:"query"`
	Items []RecallItem `json:"items"`
	Count int          `json:"count"`
}

const (
	recallDefaultLimit = 5
	recallMaxLimit     = 50
	recallTimeout      = 10 * time.Second
	recallMaxQueryLen  = 1024
)

// handleAPIRecall serves GET /api/recall?query=...&limit=...
// It performs a semantic search over historical session summaries and
// returns matching RecallItems. When the agent is nil or has no embedding
// manager, it returns an empty items array (graceful degradation).
func (ws *ReactWebServer) handleAPIRecall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query parameter is required"})
		return
	}
	if len(query) > recallMaxQueryLen {
		http.Error(w, "Query too long", http.StatusBadRequest)
		return
	}

	limit := recallDefaultLimit
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil && v > 0 {
			limit = v
			if limit > recallMaxLimit {
				limit = recallMaxLimit
			}
		}
		// on parse failure or non-positive, keep default (5)
	}

	ctx, cancel := context.WithTimeout(r.Context(), recallTimeout)
	defer cancel()

	items := []RecallItem{}
	if ws.agent != nil {
		raw, err := ws.agent.Recall(ctx, query, limit)
		if err == nil {
			for _, it := range raw {
				preview := it.Summary
				if preview == "" {
					preview = it.Actionable
				}
				items = append(items, RecallItem{
					SessionID:      it.CheckpointID,
					Workspace:      it.Workspace,
					Summary:        it.Summary,
					Actionable:     it.Actionable,
					Similarity:     it.Similarity,
					AgeDays:        it.AgeDays,
					ContentPreview: preview,
				})
			}
		}
		// on error, return empty items (graceful degradation)
	}

	writeJSON(w, http.StatusOK, RecallResponse{
		Query: query,
		Items: items,
		Count: len(items),
	})
}
