//go:build !js

package webui

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// pendingEdit tracks an edit proposal awaiting user decision.
type pendingEdit struct {
	ID           string         `json:"id"`
	Path         string         `json:"path"`
	Hunks        []editHunkInfo `json:"hunks"`
	UnifiedDiff  string         `json:"unified_diff"`
	CreatedAt    time.Time      `json:"created_at"`
	decisionCh   chan editDecisionPayload
	decisionMade bool
}

type editHunkInfo struct {
	ID       string   `json:"id"`
	Summary  string   `json:"summary"`
	AddCount int      `json:"add_count"`
	DelCount int      `json:"del_count"`
	Lines    []string `json:"lines"`
}

type editDecisionPayload struct {
	AcceptedHunks []string `json:"accepted_hunks"`
	Rejected      bool     `json:"rejected"`
}

// editRegistry tracks pending edit approvals by ID.
var editRegistry = struct {
	sync.Mutex
	pending map[string]*pendingEdit
}{pending: make(map[string]*pendingEdit)}

// handleAPIEdits dispatches /api/edits/{id} and /api/edits/{id}/decision.
func (ws *ReactWebServer) handleAPIEdits(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/decision") {
		ws.handleAPIEditDecision(w, r)
		return
	}
	ws.handleAPIEditStatus(w, r)
}

// handleAPIEditDecision handles POST /api/edits/{id}/decision — the WebUI
// submits the user's per-hunk accept/reject choices for a pending edit.
// SP-072-3: routes the decision through the agent's EditApprovalBroker
// (agent.RespondToEditApproval) which delivers to the goroutine blocked
// in RequestEditApproval. Also updates the standalone registry for
// backward compatibility with GET /api/edits/{id} status checks.
func (ws *ReactWebServer) handleAPIEditDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract edit ID from path: /api/edits/{id}/decision
	editID := extractEditIDFromPath(r.URL.Path)
	if editID == "" {
		http.Error(w, "Edit ID is required", http.StatusBadRequest)
		return
	}

	var req editDecisionPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("handleAPIEditDecision: invalid JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Build the EditDecision the agent expects.
	decision := agent.EditDecision{
		Approved:      !req.Rejected,
		AcceptedHunks: req.AcceptedHunks,
	}

	// Deliver to the agent's broker — the goroutine blocked in
	// RequestEditApproval is waiting on this channel.
	ag := ws.resolveEditAgent()
	delivered := false
	if ag != nil {
		delivered = ag.RespondToEditApproval(editID, decision)
	}

	// Also update the standalone registry for backward compatibility.
	editRegistry.Lock()
	pe, regOk := editRegistry.pending[editID]
	if regOk {
		if pe.decisionMade {
			editRegistry.Unlock()
			http.Error(w, "Edit already decided", http.StatusConflict)
			return
		}
		pe.decisionMade = true
		select {
		case pe.decisionCh <- req:
		default:
		}
	}
	editRegistry.Unlock()

	if !delivered && !regOk {
		log.Printf("handleAPIEditDecision: edit %s not found in broker or registry", editID)
		http.Error(w, "Edit not found or already decided", http.StatusNotFound)
		return
	}

	log.Printf("handleAPIEditDecision: delivered decision for edit %s (accepted=%d, rejected=%v)",
		editID, len(req.AcceptedHunks), req.Rejected)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"edit_id":  editID,
		"decided":  true,
		"accepted": len(req.AcceptedHunks),
		"rejected": req.Rejected,
	})
}

// resolveEditAgent returns an agent instance for delivering edit approval
// decisions. In shared mode (CLI+WebUI), ws.agent is the single agent.
// In daemon mode, we need any agent — since the broker is package-level,
// any instance works. We try ws.agent first, then the default chat agent.
func (ws *ReactWebServer) resolveEditAgent() *agent.Agent {
	if ws.agent != nil {
		return ws.agent
	}
	// Daemon mode: try to get any chat agent. The broker is package-level
	// so any agent instance can resolve any request ID.
	if ag, err := ws.getChatAgent(defaultWebClientID, ""); err == nil && ag != nil {
		return ag
	}
	return nil
}

// handleAPIEditStatus handles GET /api/edits/{id} — returns the current
// state of a pending edit proposal.
func (ws *ReactWebServer) handleAPIEditStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	editID := extractEditIDFromPath(r.URL.Path)
	if editID == "" {
		http.Error(w, "Edit ID is required", http.StatusBadRequest)
		return
	}

	editRegistry.Lock()
	defer editRegistry.Unlock()

	pe, ok := editRegistry.pending[editID]
	if !ok {
		http.Error(w, "Edit not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         pe.ID,
		"path":       pe.Path,
		"hunks":      pe.Hunks,
		"decided":    pe.decisionMade,
		"created_at": pe.CreatedAt,
	})
}

// extractEditIDFromPath parses /api/edits/{id}/decision or /api/edits/{id}
// and returns the {id} segment.
func extractEditIDFromPath(path string) string {
	// Expected: /api/edits/{id}/decision or /api/edits/{id}
	parts := splitPath(path)
	// parts[0]="api", parts[1]="edits", parts[2]={id}, parts[3]=optional suffix
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "edits" {
		return parts[2]
	}
	return ""
}

func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// RegisterPendingEdit creates a pending edit entry and returns a channel
// that blocks until the user submits a decision via the WebUI.
func RegisterPendingEdit(id, path string, hunks []editHunkInfo, diff string) <-chan editDecisionPayload {
	pe := &pendingEdit{
		ID:          id,
		Path:        path,
		Hunks:       hunks,
		UnifiedDiff: diff,
		CreatedAt:   time.Now(),
		decisionCh:  make(chan editDecisionPayload, 1),
	}

	editRegistry.Lock()
	editRegistry.pending[id] = pe
	editRegistry.Unlock()

	return pe.decisionCh
}

// RemovePendingEdit cleans up a pending edit entry after it's resolved
// or timed out.
func RemovePendingEdit(id string) {
	editRegistry.Lock()
	delete(editRegistry.pending, id)
	editRegistry.Unlock()
}

// WaitForEditDecision blocks until a decision is received or timeout
// elapses. Returns nil on timeout (treat as reject-all for safety).
func WaitForEditDecision(ch <-chan editDecisionPayload, timeout time.Duration) *editDecisionPayload {
	select {
	case decision := <-ch:
		return &decision
	case <-time.After(timeout):
		return nil
	}
}
