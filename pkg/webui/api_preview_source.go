//go:build !js

// api_preview_source.go exposes the HTML preview's source URL: the editor asks
// for a URL for a workspace file, and the server mints a short-lived token and
// returns the address on the preview origin (see preview_origin.go).
//
// The editor cannot build this URL itself: the preview listener binds a random
// loopback port at startup, and the token is server-minted. Returning both
// keeps the origin and its grants entirely server-side.
package webui

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// handleAPIPreviewSource serves GET /api/preview/source?path=<workspace path>.
//
// Response:
//
//	{"url": "http://127.0.0.1:PORT/__preview/<token>/<path>", "available": true}
//
// When the preview origin is unavailable (bind failed, or the server was built
// without one) it returns available=false and no url, and the editor falls back
// to a srcDoc iframe.
func (ws *ReactWebServer) handleAPIPreviewSource(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if relPath == "" {
		writeJSONErr(w, http.StatusBadRequest, "path_required", "File path is required")
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	canonical, err := canonicalizePath(relPath, workspaceRoot, false)
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, "file_not_found", fmt.Sprintf("File not found: %v", err))
		return
	}
	if !isWithinWorkspace(canonical, workspaceRoot) {
		writeJSONErr(w, http.StatusForbidden, "path_outside_workspace", "Path is outside the workspace")
		return
	}

	origin := ws.previewOrigin
	if origin == nil || origin.address() == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"reason":    "preview origin is not running",
		})
		return
	}

	token, err := origin.mintToken()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "token_mint_failed", err.Error())
		return
	}

	// The preview origin resolves paths against the workspace root, so hand it
	// the workspace-relative form rather than the absolute one.
	rel, err := workspaceRelativePath(canonical, workspaceRoot)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_file_path", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
		"url":       origin.previewURLFor(token, rel),
	})
}

// workspaceRelativePath returns canonical relative to root, rejecting a path
// that escapes it.
func workspaceRelativePath(canonical, root string) (string, error) {
	rel, err := filepath.Rel(root, canonical)
	if err != nil {
		return "", fmt.Errorf("resolving path within workspace: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path is outside the workspace")
	}
	return rel, nil
}
