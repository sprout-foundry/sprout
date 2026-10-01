//go:build !js

package webui

// search_semantic_context.go — the semantic preview-context endpoint:
// handleAPISemanticPreviewContext and its response/config types, plus the
// relativeTimeFromAgo helper. Split out of search_semantic_api.go.
import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// SemanticPreviewContextResult is one entry the proactive-context retriever
// would inject for the given query, surfaced for the Memory settings panel.
type SemanticPreviewContextResult struct {
	UserMessage  string  `json:"user_message"`  // first-line excerpt of the past turn
	Summary      string  `json:"summary"`       // actionable summary if present
	Workspace    string  `json:"workspace"`     // working directory the turn was recorded in
	Score        float64 `json:"score"`         // time-decayed cosine similarity
	RelativeTime string  `json:"relative_time"` // e.g. "3 hours ago"
}

// SemanticPreviewContextResponse mirrors the proactive-context pipeline so a
// user can see exactly what their Memory settings would inject for a given
// query before saving them.
type SemanticPreviewContextResponse struct {
	Query     string                         `json:"query"`
	Workspace string                         `json:"workspace"`
	Config    SemanticPreviewContextConfig   `json:"config"`
	Results   []SemanticPreviewContextResult `json:"results"`
	Enabled   bool                           `json:"enabled"`
	Note      string                         `json:"note,omitempty"`
}

// SemanticPreviewContextConfig echoes the resolved PersistentContext params
// the preview ran against. Useful for the UI to show "this preview used
// score>=0.50, top 5".
type SemanticPreviewContextConfig struct {
	MinRelevanceScore        float64 `json:"min_relevance_score"`
	MaxContextualResults     int     `json:"max_contextual_results"`
	MaxContextChars          int     `json:"max_context_chars"`
	WorkspaceScopedRetrieval bool    `json:"workspace_scoped_retrieval"`
}

// handleAPISemanticPreviewContext handles GET /api/search/semantic/preview-context.
// It runs the same proactive-context retrieval the agent would use, against
// the current user's PersistentContext config, and returns the top results
// for display in the Memory settings panel. Read-only: no state mutated.
func (ws *ReactWebServer) handleAPISemanticPreviewContext(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query parameter is required"})
		return
	}

	clientID := ws.resolveClientID(r)
	em := ws.getEmbeddingManager(clientID)

	// Resolve effective config: prefer the configured PersistentContext;
	// fall back to a fresh-loaded one if no manager is attached yet so the
	// preview still shows the user the defaults they would get.
	var persisted *configuration.PersistentContextConfig
	if cm := ws.getConfigManager(r, w); cm != nil {
		if cfg := cm.GetConfig(); cfg != nil {
			persisted = cfg.PersistentContext
		}
	}
	resolved := persisted.Resolve()
	cfg := agent.ProactiveContextConfig{
		MinRelevanceScore:    resolved.MinRelevanceScore,
		MaxContextualResults: resolved.MaxContextualResults,
		MaxContextChars:      resolved.MaxContextChars,
		WorkspaceScoped:      resolved.WorkspaceScopedRetrieval,
		RetentionDays:        resolved.RetentionDays,
	}

	workspaceRoot := ws.GetWorkspaceRoot()
	now := time.Now().UTC()

	resp := SemanticPreviewContextResponse{
		Query:     query,
		Workspace: workspaceRoot,
		Enabled:   resolved.ProactiveContextEnabled,
		Config: SemanticPreviewContextConfig{
			MinRelevanceScore:        resolved.MinRelevanceScore,
			MaxContextualResults:     resolved.MaxContextualResults,
			MaxContextChars:          resolved.MaxContextChars,
			WorkspaceScopedRetrieval: resolved.WorkspaceScopedRetrieval,
		},
		Results: []SemanticPreviewContextResult{},
	}

	if !resolved.ProactiveContextEnabled {
		resp.Note = "Proactive context is disabled in settings — no preview will be retrieved."
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if em == nil {
		resp.Note = "Embedding manager is not initialized for this session."
		writeJSON(w, http.StatusOK, resp)
		return
	}

	results, err := agent.RetrieveProactiveContext(r.Context(), em, cfg, query, workspaceRoot, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("preview retrieval failed: %v", err)})
		return
	}

	for _, res := range results {
		rec := res.Record
		summary := ""
		if s, ok := rec.Metadata["actionableSummary"].(string); ok {
			summary = s
		}
		ws := ""
		if v, ok := rec.Metadata["workingDir"].(string); ok {
			ws = v
		}
		// First-line excerpt — same trim/truncate FormatProactiveContext uses.
		userMsg := rec.Signature
		if idx := strings.Index(userMsg, "\n"); idx >= 0 {
			userMsg = userMsg[:idx]
		}
		if len(userMsg) > 200 {
			runes := []rune(userMsg)
			if len(runes) > 200 {
				userMsg = string(runes[:197]) + "..."
			}
		}
		resp.Results = append(resp.Results, SemanticPreviewContextResult{
			UserMessage:  userMsg,
			Summary:      summary,
			Workspace:    ws,
			Score:        res.Score,
			RelativeTime: relativeTimeFromAgo(rec.IndexedAt, now),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// relativeTimeFromAgo formats a past timestamp as a coarse human string.
// Duplicates a small slice of pkg/agent.formatRelativeTime so we don't have
// to widen its package visibility just for one consumer; if more callers
// appear, hoist the agent helper to pkg/agent.FormatRelativeTime instead.
func relativeTimeFromAgo(t time.Time, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d weeks ago", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%d months ago", int(d.Hours()/(24*30)))
	}
}
