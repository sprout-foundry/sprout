//go:build !js

package webui

import (
	"encoding/json"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// handleAPISyncOp handles POST /api/sync/op — apply a single SyncOp to the workspace.
func (ws *ReactWebServer) handleAPISyncOp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB

	var op agent.SyncOp
	if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if op.Path == "" {
		writeJSONErr(w, http.StatusBadRequest, "path_required", "path must not be empty")
		return
	}

	if op.OpType == "rename" && op.NewPath == "" {
		writeJSONErr(w, http.StatusBadRequest, "new_path_required", "new_path must not be empty for rename operation")
		return
	}

	// Resolve the agent for this request. Prefer the server-level agent
	// (ws.agent) for workspace-level operations like sync — the file metadata
	// tracked by ApplySyncOp lives on the agent instance that created the
	// files, which is ws.agent in CLI mode. In daemon mode (ws.agent == nil),
	// fall back to the per-client agent.
	ag := ws.agent
	if ag == nil {
		ag = ws.getActiveAgentForRequest(r)
	}
	if ag == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_not_initialized", "agent not initialized")
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	result := ag.ApplySyncOp(op, workspaceRoot)

	w.Header().Set("Content-Type", "application/json")
	if !result.Accepted {
		if result.ConflictPath != "" {
			w.WriteHeader(http.StatusConflict)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	}
	json.NewEncoder(w).Encode(result)
}

// handleAPISyncBatch handles POST /api/sync/batch — apply a batch of SyncOps.
func (ws *ReactWebServer) handleAPISyncBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 50<<20) // 50 MB

	var req struct {
		Ops []agent.SyncOp `json:"ops"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if len(req.Ops) < 1 {
		writeJSONErr(w, http.StatusBadRequest, "operations_required", "at least one operation is required")
		return
	}

	// Resolve the agent for this request. Prefer the server-level agent
	// (ws.agent) for workspace-level operations like sync — the file metadata
	// tracked by ApplySyncOp lives on the agent instance that created the
	// files, which is ws.agent in CLI mode. In daemon mode (ws.agent == nil),
	// fall back to the per-client agent.
	ag := ws.agent
	if ag == nil {
		ag = ws.getActiveAgentForRequest(r)
	}
	if ag == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_not_initialized", "agent not initialized")
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	results := ag.ApplySyncOpBatch(req.Ops, workspaceRoot)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
	})
}

// handleAPISyncStatus handles GET /api/sync/status — return current sync state.
func (ws *ReactWebServer) handleAPISyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Resolve the agent for this request. Prefer the server-level agent
	// (ws.agent) for workspace-level operations like sync. In daemon mode
	// (ws.agent == nil), fall back to the per-client agent.
	ag := ws.agent
	if ag == nil {
		ag = ws.getActiveAgentForRequest(r)
	}
	if ag == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"files": interface{}(nil),
		})
		return
	}

	files := ag.GetSyncStatus()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"files": files,
	})
}
