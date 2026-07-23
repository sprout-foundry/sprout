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
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// pendingShellApproval tracks a shell approval awaiting user decision.
type pendingShellApproval struct {
	ID           string
	Command      string
	Parts        []agent.ShellPart
	CreatedAt    time.Time
	decisionsCh  chan map[string]bool
	decisionMade bool
}

// shellApprovalRegistry tracks pending shell approvals by ID.
var shellApprovalRegistry = struct {
	sync.Mutex
	pending map[string]*pendingShellApproval
}{pending: make(map[string]*pendingShellApproval)}

// RegisterShellApproval stores a pending approval and returns a channel
// that will receive the per-part decisions map when the WebUI responds.
// Returns nil if the request ID is already registered (deduplication).
func RegisterShellApproval(id, command string, parts []agent.ShellPart) chan map[string]bool {
	shellApprovalRegistry.Lock()
	defer shellApprovalRegistry.Unlock()
	if _, ok := shellApprovalRegistry.pending[id]; ok {
		return nil
	}
	p := &pendingShellApproval{
		ID:          id,
		Command:     command,
		Parts:       parts,
		CreatedAt:   time.Now(),
		decisionsCh: make(chan map[string]bool, 1),
	}
	shellApprovalRegistry.pending[id] = p
	return p.decisionsCh
}

// handleAPIShellApprovals dispatches /api/shell-approvals/{id}/decision.
func (ws *ReactWebServer) handleAPIShellApprovals(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/decision") {
		ws.handleAPIShellApprovalDecision(w, r)
		return
	}
	http.Error(w, "Not found", http.StatusNotFound)
}

// handleAPIShellApprovalDecision handles POST /api/shell-approvals/{id}/decision
// — the WebUI submits per-part accept/reject choices for a pending shell approval.
// SP-093-3: unblocks the broker by delivering the decisions map to the channel
// returned by RegisterShellApproval.
func (ws *ReactWebServer) handleAPIShellApprovalDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := extractShellApprovalIDFromPath(r.URL.Path)
	if id == "" {
		http.Error(w, "Request ID required", http.StatusBadRequest)
		return
	}
	var req events.ShellApprovalResponsePayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ws.log().Warn("invalid shell approval decision JSON", slog.Any("err", err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.RequestID == "" {
		req.RequestID = id
	}
	if req.Decisions == nil {
		http.Error(w, "decisions map required", http.StatusBadRequest)
		return
	}
	shellApprovalRegistry.Lock()
	p, ok := shellApprovalRegistry.pending[id]
	if ok && !p.decisionMade {
		p.decisionMade = true
		select {
		case p.decisionsCh <- req.Decisions:
		default:
		}
		delete(shellApprovalRegistry.pending, id)
	}
	shellApprovalRegistry.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "request_id": id})
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
