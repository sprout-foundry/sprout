//go:build !js

package webui

// automations_sessions_api.go — automate session operations, split out of
// automations_api.go. handleAPIAutomateSessionsAll is the catch-all
// dispatcher for /api/automate/sessions/ and routes to the single-session,
// stop, and output handlers; those read session files, probe live process
// status, send escalating stop signals, and stream output with byte-offset
// resumption.
import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/automate"
)

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
	if !requireMethod(w, r, http.MethodGet) {
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
	if !requireMethod(w, r, http.MethodPost) {
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

	if info.EndedAt != nil {
		// Finalized record — retained for post-mortem observability.
		// Status maps to the frontend's union ('running' | 'exited' |
		// 'stopped'); the raw outcome travels in exit_code.
		exit := 0
		if info.ExitCode != nil {
			exit = *info.ExitCode
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID,
			"status":     "exited",
			"exit_code":  exit,
			"stopped":    false,
		})
		return
	}

	if !automate.IsProcessAlive(info.PID) {
		// Dead but never finalized (parent killed before its defer ran) —
		// finalize with -1 rather than deleting, preserving the post-mortem.
		_ = automate.FinalizeSessionFile(sproutDir, sessionID, -1)
		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID,
			"status":     "exited",
			"exit_code":  -1,
			"stopped":    false,
		})
		return
	}

	// Stop the process if it's still alive.
	if automate.IsProcessAlive(info.PID) {
		// Process stop is best-effort; session file cleanup proceeds regardless.
		_, _ = automate.StopProcess(info.PID)
	}

	// Clean up the session file.
	_ = automate.RemoveSessionFile(sproutDir, sessionID)

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"status":     "stopped",
		"stopped":    true,
	})
}

// handleAPIAutomateSessionOutput reads the output file for a session.
// Supports a "since" query param for byte-offset resumption.
func (ws *ReactWebServer) handleAPIAutomateSessionOutput(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !requireMethod(w, r, http.MethodGet) {
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
