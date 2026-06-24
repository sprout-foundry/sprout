//go:build !js

// Agent-changes HTTP API.
//
// Exposes the ChangeTracker's session buffer to the WebUI as JSON
// endpoints. Each endpoint mirrors the LLM-facing tool of the same
// name and returns the same JSON shape — that way the frontend and
// the model agree on the data they're reasoning about.
//
//	GET  /api/changes/session   — current manifest (list_changes output)
//	GET  /api/changes/diff      — unified diff for one file (show_my_change)
//	GET  /api/changes/summary   — grouped activity-block digest (summarize_my_session)
//	GET  /api/changes/timeline  — cross-session timeline (my_recent_changes)
//	POST /api/changes/revert    — bulk undo with scope (revert_my_changes)
//
// All endpoints resolve the calling client's Agent via the existing
// client-context pattern so the panel reflects state for THAT browser
// session's agent (multi-client / multi-workspace safe).
package webui

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func (ws *ReactWebServer) registerChangesRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/changes/session", ws.handleAPIChangesSession)
	mux.HandleFunc("/api/changes/diff", ws.handleAPIChangesDiff)
	mux.HandleFunc("/api/changes/summary", ws.handleAPIChangesSummary)
	mux.HandleFunc("/api/changes/timeline", ws.handleAPIChangesTimeline)
	mux.HandleFunc("/api/changes/revert", ws.handleAPIChangesRevert)
}

// handleAPIChangesSession returns the current session manifest. Mirrors
// the LLM tool list_changes; supports the same since / tool /
// path_pattern query params.
//
// When no live agent exists (daemon mode, browser opened before first
// chat query), it falls back to the persisted history store so the
// panel still shows cross-session change history rather than a 503.
func (ws *ReactWebServer) handleAPIChangesSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)

	args := map[string]interface{}{}
	q := r.URL.Query()
	if v := q.Get("since"); v != "" {
		args["since"] = v
	}
	if v := q.Get("tool"); v != "" {
		args["tool"] = v
	}
	if v := q.Get("path_pattern"); v != "" {
		args["path_pattern"] = v
	}

	if err != nil || agentInst == nil {
		// No live agent — fall back to persisted-only history.
		out, perr := agent.ListChangesPersistedOnly(args)
		if perr != nil {
			writeChangesError(w, http.StatusInternalServerError, perr.Error())
			return
		}
		writeChangesJSON(w, out)
		return
	}

	out, err := agentInst.ListChanges(args)
	if err != nil {
		writeChangesError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeChangesJSON(w, out)
}

// handleAPIChangesDiff returns the unified diff for one file. Requires
// a `path` query param. Mirrors show_my_change.
//
// Without a live agent, diffs aren't computable (they require the
// in-memory tracker's before/after content). Returns an empty
// not-found envelope rather than a 503 so the panel degrades
// gracefully.
func (ws *ReactWebServer) handleAPIChangesDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeChangesError(w, http.StatusBadRequest, "'path' query parameter is required")
		return
	}
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeChangesJSON(w, fmt.Sprintf(`{"found":false,"path":%q}`, path))
		return
	}
	out, err := agentInst.ShowMyChange(path)
	if err != nil {
		writeChangesError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeChangesJSON(w, out)
}

// handleAPIChangesSummary returns the grouped activity-block digest.
// Mirrors summarize_my_session — no parameters.
//
// Without a live agent, returns an empty disabled response so the
// panel shows "no changes this session" instead of a 503 error.
func (ws *ReactWebServer) handleAPIChangesSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeChangesJSON(w, agent.SummarizeMySessionEmpty())
		return
	}
	out, err := agentInst.SummarizeMySession()
	if err != nil {
		writeChangesError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeChangesJSON(w, out)
}

// handleAPIChangesTimeline returns the cross-session unified timeline.
// Mirrors my_recent_changes; accepts ?since=<duration|RFC3339>.
//
// The timeline is persisted-history based, so it works even without
// a live agent. When there IS an agent, it merges session-scoped
// in-memory entries as well.
func (ws *ReactWebServer) handleAPIChangesTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		// No live agent — use the persisted-only path directly.
		args := map[string]interface{}{"include_persisted": true}
		if v := r.URL.Query().Get("since"); v != "" {
			args["since"] = v
		}
		out, perr := agent.ListChangesPersistedOnly(args)
		if perr != nil {
			writeChangesError(w, http.StatusInternalServerError, perr.Error())
			return
		}
		writeChangesJSON(w, out)
		return
	}
	out, err := agentInst.MyRecentChanges(r.URL.Query().Get("since"))
	if err != nil {
		writeChangesError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeChangesJSON(w, out)
}

// handleAPIChangesRevert performs a bulk revert. Body shape mirrors
// the revert_my_changes tool: { scope?, file?, since? }.
func (ws *ReactWebServer) handleAPIChangesRevert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Scope string `json:"scope,omitempty"`
		File  string `json:"file,omitempty"`
		Since string `json:"since,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChangesError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeChangesError(w, http.StatusConflict, "No active agent session to revert changes from")
		return
	}

	out, err := agentInst.RevertMyChanges(req.Scope, req.File, req.Since)
	if err != nil {
		writeChangesError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Surface the revert as a client event too so any other tab the
	// user has open immediately refreshes its panel.
	ws.publishClientEvent(clientID, "agent_changes_reverted", map[string]interface{}{
		"scope":     req.Scope,
		"file":      req.File,
		"since":     req.Since,
		"client_id": clientID,
	})

	writeChangesJSON(w, out)
}

func writeChangesJSON(w http.ResponseWriter, raw string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func writeChangesError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg})
}
