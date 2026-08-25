//go:build !js

// Package webui provides the React-based web UI server for the Sprout AI agent.
//
// This file implements the shell approval API endpoints for SP-093-3,
// enabling per-part approval of shell commands via the WebUI.

package webui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// handleAPIShellApprovals dispatches /api/shell-approvals/{id}/decision.
func (ws *ReactWebServer) handleAPIShellApprovals(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/decision") {
		ws.handleAPIShellApprovalDecision(w, r)
		return
	}
	writeJSONErr(w, http.StatusNotFound, "not_found", "Not found")
}

// handleAPIShellApprovalDecision handles POST /api/shell-approvals/{id}/decision
// — the WebUI submits per-part accept/reject choices for a pending shell approval.
// SP-093-3: unblocks the broker by delivering the decisions map to the channel
// returned by RegisterShellApproval.
func (ws *ReactWebServer) handleAPIShellApprovalDecision(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	id := extractShellApprovalIDFromPath(r.URL.Path)
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "request_id_required", "Request ID required")
		return
	}
	var req events.ShellApprovalResponsePayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ws.log().Warn("invalid shell approval decision JSON", slog.Any("err", err))
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	if req.RequestID == "" {
		req.RequestID = id
	}
	if req.Decisions == nil {
		writeJSONErr(w, http.StatusBadRequest, "decisions_map_required", "decisions map required")
		return
	}

	// Deliver the decisions to the blocked agent goroutine via the
	// package-level broker. Mirrors the edit-approval pattern: try
	// ws.agent first (shared CLI+WebUI mode), then the daemon chat agent.
	ag := ws.resolveShellApprovalAgent()
	if ag == nil {
		ws.log().Warn("shell approval decision: no agent available",
			slog.String("request_id", id))
		writeJSONErr(w, http.StatusServiceUnavailable, "no_agent_available", "no agent available to receive decision")
		return
	}
	if !ag.RespondToShellApproval(id, req.Decisions) {
		ws.log().Warn("shell approval decision not delivered (unknown/expired request ID)",
			slog.String("request_id", id))
		writeJSONErr(w, http.StatusGone, "decision_not_delivered", "decision not delivered (unknown or expired request)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "request_id": id, "delivered": true})
}

// resolveShellApprovalAgent returns an agent instance for delivering shell
// approval decisions. Mirrors resolveEditAgent: ws.agent in shared mode,
// any chat agent in daemon mode (the broker is package-level).
func (ws *ReactWebServer) resolveShellApprovalAgent() *agent.Agent {
	if ws.agent != nil {
		return ws.agent
	}
	if ag, err := ws.getChatAgent(defaultWebClientID, ""); err == nil && ag != nil {
		return ag
	}
	return nil
}

// extractShellApprovalIDFromPath extracts the ID from
// /api/shell-approvals/{id}/decision.
func extractShellApprovalIDFromPath(path string) string {
	const prefix = "/api/shell-approvals/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimSuffix(rest, "/decision")
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return ""
	}
	return rest
}
