//go:build !js

// Agent-automate HTTP API.
//
// Exposes workflow discovery, session tracking, and launch controls to
// the WebUI automate panel. Endpoints mirror the tool layer so the
// frontend can interact with automate workflows without going through
// the chat interface.
//
//	GET    /api/automate/workflows           — list available workflows
//	GET    /api/automate/sessions             — list all automate sessions
//	GET    /api/automate/sessions/:id         — single session detail
//	POST   /api/automate/run                  — launch a workflow
//	POST   /api/automate/sessions/:id/stop    — stop a running workflow
//	GET    /api/automate/sessions/:id/output  — read workflow output
package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/automate"
)

// sessionResponse is the JSON shape returned for automate session
// detail and list endpoints, enriched with live process status.
type sessionResponse struct {
	SessionID      string     `json:"session_id,omitempty"`
	Workflow       string     `json:"workflow"`
	PID            int        `json:"pid"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	Kind           string     `json:"kind"`
	OutputFilePath string     `json:"output_file_path,omitempty"`
	BudgetUSD      *float64   `json:"budget_usd,omitempty"`
}

// registerAutomateRoutes adds the automate panel endpoints to the mux.
func (ws *ReactWebServer) registerAutomateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/automate/workflows", ws.handleAPIAutomateWorkflows)
	mux.HandleFunc("/api/automate/sessions", ws.handleAPIAutomateSessionsList)
	mux.HandleFunc("/api/automate/sessions/", ws.handleAPIAutomateSessionsAll)
	mux.HandleFunc("/api/automate/run", ws.handleAPIAutomateRun)
}

// handleAPIAutomateWorkflows lists available workflow files from the automate/
// directory. Returns an empty array (not an error) if the directory doesn't
// exist yet.
func (ws *ReactWebServer) handleAPIAutomateWorkflows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type workflowItem struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Filename    string `json:"filename"`
		FilePath    string `json:"file_path"`
	}

	workflows, err := automate.Discover(automate.Dir())
	if err != nil {
		if automate.IsNotExists(err) {
			writeJSON(w, http.StatusOK, map[string]any{"workflows": []workflowItem{}})
			return
		}
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan automate directory: %v", err))
		return
	}

	items := make([]workflowItem, 0, len(workflows))
	for _, wf := range workflows {
		items = append(items, workflowItem{
			Name:        wf.Filename,
			Description: wf.Description,
			Filename:    wf.Filename,
			FilePath:    wf.FilePath,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"workflows": items})
}

// handleAPIAutomateSessionsList returns every automate session from
// .sprout/automate/, enriched with live process status.
func (ws *ReactWebServer) handleAPIAutomateSessionsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sproutDir := ws.getSproutDir(r)
	sessions, err := automate.ListSessionFiles(sproutDir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list sessions: %v", err))
		return
	}

	enriched := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		enriched = append(enriched, makeSessionResponse(s))
	}

	writeJSON(w, http.StatusOK, map[string]any{"sessions": enriched})
}

// handleAPIAutomateSessionsAll is the catch-all handler for the
// /api/automate/sessions/ prefix. It dispatches to:
//
// - GET  /api/automate/sessions/:id         → single session
// - POST /api/automate/sessions/:id/stop    → stop session
// - GET  /api/automate/sessions/:id/output  → read output
func (ws *ReactWebServer) handleAPIAutomateSessionsAll(w http.ResponseWriter, r *http.Request) {
	// Extract the remainder after the prefix.
	rem := strings.TrimPrefix(r.URL.Path, "/api/automate/sessions/")
	rem = strings.TrimSuffix(rem, "/")
	// Strip query string.
	if i := strings.Index(rem, "?"); i >= 0 {
		rem = rem[:i]
	}

	// Determine action.
	switch {
	case rem == "":
		// No ID after prefix — treat as list.
		ws.handleAPIAutomateSessionsList(w, r)
	case strings.HasSuffix(rem, "/stop"):
		sessionID := strings.TrimSuffix(rem, "/stop")
		ws.handleAPIAutomateSessionStop(w, r, sessionID)
	case strings.HasSuffix(rem, "/output"):
		sessionID := strings.TrimSuffix(rem, "/output")
		ws.handleAPIAutomateSessionOutput(w, r, sessionID)
	default:
		// Plain session ID — return single session detail.
		ws.handleAPIAutomateSessionSingle(w, r, rem)
	}
}

// handleAPIAutomateSessionSingle returns one session by ID.
func (ws *ReactWebServer) handleAPIAutomateSessionSingle(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// Reject session IDs containing path separators or traversal sequences.
	if strings.Contains(sessionID, "/") || strings.Contains(sessionID, "\\") || strings.Contains(sessionID, "..") {
		writeJSONError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	sproutDir := ws.getSproutDir(r)
	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
		return
	}

	resp := makeSessionResponse(*info)
	resp.SessionID = sessionID
	writeJSON(w, http.StatusOK, resp)
}

// handleAPIAutomateSessionStop sends escalating signals to stop the
// tracked process, then removes the session file.
func (ws *ReactWebServer) handleAPIAutomateSessionStop(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// Reject session IDs containing path separators or traversal sequences.
	if strings.Contains(sessionID, "/") || strings.Contains(sessionID, "\\") || strings.Contains(sessionID, "..") {
		writeJSONError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	sproutDir := ws.getSproutDir(r)
	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}

	// Stop the process if it's still alive.
	if automate.IsProcessAlive(info.PID) {
		// Process stop is best-effort; session file cleanup proceeds regardless.
		_, _ = automate.StopProcess(info.PID)
	}

	// Clean up the session file.
	_ = automate.RemoveSessionFile(sproutDir, sessionID)

	writeJSON(w, http.StatusOK, map[string]string{
		"session_id": sessionID,
		"status":     "stopped",
	})
}

// handleAPIAutomateSessionOutput reads the output file for a session.
// Supports a "since" query param for byte-offset resumption.
func (ws *ReactWebServer) handleAPIAutomateSessionOutput(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// Reject session IDs containing path separators or traversal sequences.
	if strings.Contains(sessionID, "/") || strings.Contains(sessionID, "\\") || strings.Contains(sessionID, "..") {
		writeJSONError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	sproutDir := ws.getSproutDir(r)
	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}

	if info.OutputFilePath == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"output": "",
			"offset": 0,
			"total":  0,
		})
		return
	}

	// Validate output file path stays within workspace.
	absOutput, err := filepath.Abs(info.OutputFilePath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid output file path")
		return
	}
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot != "" {
		absWorkspace, _ := filepath.Abs(workspaceRoot)
		// Resolve symlinks in both paths so macOS /var → /private/var
		// doesn't cause a false negative.
		if evaled, err := filepath.EvalSymlinks(absWorkspace); err == nil {
			absWorkspace = evaled
		}
		if evaled, err := filepath.EvalSymlinks(absOutput); err == nil {
			absOutput = evaled
		}
		// Add trailing separator to avoid prefix mismatches like
		// "/tmp/ws2" matching "/tmp/ws".
		if !strings.HasPrefix(absOutput, absWorkspace+string(filepath.Separator)) {
			writeJSONError(w, http.StatusBadRequest, "output file path outside workspace")
			return
		}
	}

	// Parse optional byte-offset resume cursor.
	offset := 0
	if since := r.URL.Query().Get("since"); since != "" {
		if o, err := strconv.Atoi(since); err == nil && o >= 0 {
			offset = o
		}
	}

	f, err := os.Open(info.OutputFilePath)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("output file not found: %v", err))
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to stat output file")
		return
	}

	total := int(fi.Size())

	// Seek to offset if requested.
	if offset > 0 {
		if offset > total {
			// Past EOF — return empty.
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"output": "",
				"offset": total,
				"total":  total,
			})
			return
		}
		_, err = f.Seek(int64(offset), 0)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to seek output file")
			return
		}
	}

	remaining := total - offset
	if remaining <= 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"output": "",
			"offset": total,
			"total":  total,
		})
		return
	}

	buf := make([]byte, remaining)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusInternalServerError, "failed to read output file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"output": string(buf[:n]),
		"offset": offset + n,
		"total":  total,
	})
}

// handleAPIAutomateRun launches a workflow after checking approval requirements.
//
// If the workflow requires approval, returns 200 with {requires_approval: true}
// so the frontend can show a confirmation dialog. On success, returns the
// session info from the tool layer.
func (ws *ReactWebServer) handleAPIAutomateRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Workflow     string   `json:"workflow"`
		BudgetUSD    *float64 `json:"budget_usd,omitempty"`
		BudgetWarn   *string  `json:"budget_warn,omitempty"`
		Heartbeat    *int     `json:"heartbeat,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Workflow == "" {
		writeJSONError(w, http.StatusBadRequest, "workflow name is required")
		return
	}

	// Validate the workflow file exists (path traversal protection is in ResolvePath).
	dir := automate.Dir()
	if _, err := automate.ResolvePath(dir, req.Workflow); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid workflow: %v", err))
		return
	}

	// Check approval requirement. If approval is required, return a JSON
	// response (not an error) so the frontend can show a confirmation prompt.
	if agent.WorkflowRequiresApproval(req.Workflow) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"requires_approval": true,
			"workflow":          req.Workflow,
		})
		return
	}

	// Resolve the client's agent to execute the workflow.
	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "no agent available for client")
		return
	}

	result, err := agentInst.RunAutomateWorkflow(r.Context(), req.Workflow)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Parse the JSON result from the tool layer and return it.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		// If parsing fails, return raw string.
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"result": result,
		})
		return
	}

	writeJSON(w, http.StatusOK, parsed)
}

// --- helpers ---

// getSproutDir resolves the .sprout directory for the current client's
// workspace, falling back to the CWD.
func (ws *ReactWebServer) getSproutDir(r *http.Request) string {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "."
		}
		workspaceRoot = wd
	}
	return filepath.Join(workspaceRoot, ".sprout")
}

// makeSessionResponse enriches a raw session info with live process status.
func makeSessionResponse(s automate.AutomateSessionInfo) sessionResponse {
	status := "exited"
	if automate.IsProcessAlive(s.PID) {
		status = "running"
	}
	return sessionResponse{
		Workflow:       s.Workflow,
		PID:            s.PID,
		Status:         status,
		StartedAt:      s.StartedAt,
		Kind:           s.Kind,
		OutputFilePath: s.OutputFilePath,
		BudgetUSD:      s.BudgetUSD,
	}
}
