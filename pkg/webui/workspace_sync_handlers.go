//go:build !js

// Package webui provides the React web server with embedded assets.
//
// This file implements the workspace sync HTTP handler for
// POST /api/workspace/sync (SP-046).
// See roadmap/SP-046-workspace-sync-model.md for the full specification.

package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// workspaceSyncState is the package-level sync state for the workspace sync
// protocol. It is initialized once at startup and shared across all
// ReactWebServer instances. In a multi-tenant future, this would be keyed by
// user or workspace.
var workspaceSyncState = agenttools.NewSyncState()

// activeSessionRegistry is the package-level multi-device session registry for
// SP-046-5. It tracks which device currently holds an active session.
var activeSessionRegistry = NewActiveSessionRegistry()

// syncRequest is the JSON body shape for POST /api/workspace/sync.
type syncRequest struct {
	// Op is the operation type. Currently only "patch" is supported.
	Op string `json:"op"`

	// Path is the workspace-relative file path (e.g. "pkg/foo/bar.go").
	Path string `json:"path"`

	// Content is the full file content after the browser edit.
	Content string `json:"content"`

	// BrowserSeq is the browser's sequence number after this edit.
	BrowserSeq int64 `json:"browser_seq"`

	// LastSyncedContainer is the last container_seq the browser has seen for
	// this file, used for staleness detection.
	LastSyncedContainer int64 `json:"last_synced_container"`
}

// handleAPIWorkspaceSync handles POST /api/workspace/sync.
//
// Accepts a browser→container patch operation: the user edited a file in the
// browser editor, and this call syncs the change to the server-side state.
//
// On success (200), returns the updated FileMetadata for the file.
// On conflict (409), returns an error with the path to the ".theirs" file.
// On bad request (400), returns an error describing what was wrong.
func (ws *ReactWebServer) handleAPIWorkspaceSync(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB max

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "failed_to_read_request_body", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		writeJSONErr(w, http.StatusBadRequest, "request_body_required", "request body is required")
		return
	}

	var req syncRequest
	if err := json.Unmarshal(body, &req); err != nil {
		ws.log().Warn("invalid workspace sync request", slog.Any("err", err))
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if req.Op != "patch" {
		writeJSONErr(w, http.StatusBadRequest, "unsupported_op", fmt.Sprintf("unsupported op %q, only 'patch' is supported", req.Op))
		return
	}

	if req.Path == "" {
		writeJSONErr(w, http.StatusBadRequest, "path_required", "path is required")
		return
	}

	// Apply the browser operation to the sync state.
	metadata, err := workspaceSyncState.ApplyBrowserOp(req.Path, req.Content)
	if err != nil {
		ws.log().Warn("workspace sync conflict", slog.String("path", req.Path), slog.Any("err", err))
		// On conflict, suggest a .theirs path for the browser to surface.
		theirsPath := req.Path + ".theirs"
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":       err.Error(),
			"theirs_path": theirsPath,
		})
		return
	}

	writeJSON(w, http.StatusOK, metadata)
}

// takeoverRequest is the JSON body shape for POST /api/workspace/takeover.
type takeoverRequest struct {
	SessionID string `json:"session_id"`
	DeviceID  string `json:"device_id"`
}

// handleAPIWorkspaceTakeover handles POST /api/workspace/takeover (SP-046-5).
//
// A new device requests to take over an existing session. If the session is
// already active on another device, the old device is swapped out and a
// workspace.session_moved event is published so the displaced browser can
// surface the overlay.
func (ws *ReactWebServer) handleAPIWorkspaceTakeover(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024) // small body

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "failed_to_read_request_body", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		writeJSONErr(w, http.StatusBadRequest, "request_body_required", "request body is required")
		return
	}

	var req takeoverRequest
	if err := json.Unmarshal(body, &req); err != nil {
		ws.log().Warn("invalid workspace takeover request", slog.Any("err", err))
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if req.SessionID == "" || req.DeviceID == "" {
		writeJSONErr(w, http.StatusBadRequest, "session_id_and_device_id_required", "session_id and device_id are required")
		return
	}

	// Atomically swap the active device.
	oldDevice := activeSessionRegistry.RequestTakeover(req.SessionID, req.DeviceID)

	if oldDevice != "" {
		// Publish session_moved event so the displaced browser can show the overlay.
		ws.eventBus.Publish(events.EventTypeWorkspaceSessionMoved, map[string]interface{}{
			"session_id":      req.SessionID,
			"previous_device": oldDevice,
			"new_device":      req.DeviceID,
		})

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"taken_over":      true,
			"previous_device": oldDevice,
		})
	} else {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"taken_over": false,
		})
	}
}
