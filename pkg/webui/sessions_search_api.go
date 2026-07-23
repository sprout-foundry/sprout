//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sprout-foundry/sprout/pkg/search"
)

// handleAPISessionsSearch provides cross-session search via HTTP.
// GET /api/sessions/search?q=...&cwd=...&since=...&until=...&limit=...&reindex=...
//
// Query parameters:
//   - q (required): search query string
//   - cwd: restrict to sessions in a specific working directory
//   - since: only sessions with LastUpdated >= date (RFC3339 or YYYY-MM-DD)
//   - until: only sessions with LastUpdated <= date
//   - limit: max results (default 20)
//   - reindex: force full index rebuild ("true" or "1")
//
// Response:
//
//	{
//	  "query": "...",
//	  "total": N,
//	  "results": [
//	    {
//	      "session_id": "...",
//	      "name": "...",
//	      "working_directory": "...",
//	      "last_updated": "2024-01-01T00:00:00Z",
//	      "total_cost": 0.12,
//	      "excerpt": "...",
//	      "match_score": 3
//	    }
//	  ]
//	}
func (ws *ReactWebServer) handleAPISessionsSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" && !r.URL.Query().Has("q") {
		writeJSONError(w, http.StatusBadRequest, "missing q parameter")
		return
	}

	// Parse optional parameters
	cwd := r.URL.Query().Get("cwd")
	sinceStr := r.URL.Query().Get("since")
	untilStr := r.URL.Query().Get("until")
	limitStr := r.URL.Query().Get("limit")
	reindexStr := r.URL.Query().Get("reindex")

	// Build search options
	opts := search.SearchOptions{
		Query:      q,
		WorkingDir: cwd,
	}

	// Parse date filters
	if sinceStr != "" {
		t, err := parseSearchDateWebUI(sinceStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid since date: %v", err))
			return
		}
		opts.Since = t
	}
	if untilStr != "" {
		t, err := parseSearchDateWebUI(untilStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid until date: %v", err))
			return
		}
		opts.Until = t
	}

	// Parse limit
	if limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid limit: %v", err))
			return
		}
		opts.Limit = limit
	}

	// Check if reindex is requested
	reindex := reindexStr == "true" || reindexStr == "1"

	// Load existing index; on corrupt data, fall through to rebuild.
	indexPath := search.DefaultIndexPath()
	if indexPath == "" {
		writeJSONError(w, http.StatusInternalServerError, "could not determine home directory")
		return
	}

	idx, err := search.LoadIndex(indexPath)
	if err != nil {
		ws.log().Warn("search index is corrupt; rebuilding", slog.Any("err", err))
		idx = nil
	}

	// Build index if reindex requested, index is empty, or load failed (corrupt).
	if reindex || idx == nil || len(idx.Sessions) == 0 {
		sessionsDir := filepath.Join(filepath.Dir(indexPath), "scoped")
		buildIdx := idx
		if reindex {
			buildIdx = nil // Force full rebuild, don't use cached entries
		}
		idx, err = search.BuildIndex(sessionsDir, buildIdx)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("build search index: %v", err))
			return
		}
		if err := search.SaveIndex(indexPath, idx); err != nil {
			ws.log().Warn("failed to save search index", slog.Any("err", err))
			// Continue with in-memory index — search still works
		}
	}

	// Execute search
	results := search.Search(idx, opts)
	if results == nil {
		results = []search.SearchResult{}
	}

	// Return results
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":   q,
		"total":   len(results),
		"results": results,
	})
}

// parseSearchDateWebUI tries RFC3339 first, then falls back to YYYY-MM-DD.
func parseSearchDateWebUI(date string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, date)
	if err == nil {
		return t, nil
	}
	t, err = time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (expected RFC3339 or YYYY-MM-DD)", date)
	}
	return t, nil
}
