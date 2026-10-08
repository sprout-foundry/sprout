//go:build !js

package webui

import (
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/filediscovery"
)

// handleAPIFileIndex serves GET /api/file-index — the workspace's whole
// quick-open index in ONE response. The command palette previously crawled
// the tree one /api/browse request per directory (hundreds of serial
// round-trips on real repos); this endpoint walks the tree server-side with
// the same ignore rules and caps and returns every row at once.
//
// Rows are workspace-relative paths: the response carries no absolute paths,
// so a cached index can never leak rows across a workspace switch.
func (ws *ReactWebServer) handleAPIFileIndex(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		writeJSONErr(w, http.StatusBadRequest, "workspace_not_selected", "No workspace selected")
		return
	}
	// Same home-workspace hardening as /api/browse and /api/files (SP-130):
	// walking $HOME raises macOS TCC prompts and is what the workspace gate
	// exists to prevent.
	if isHomeWorkspace(workspaceRoot) && !hasHomeWorkspaceConsent() {
		writeJSONErr(w, http.StatusForbidden, "workspace_not_selected",
			"Workspace is the home directory and no consent was granted; select a workspace first")
		return
	}

	result := filediscovery.BuildFileIndex(workspaceRoot, filediscovery.DefaultIndexLimits())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "success",
		"workspace":    workspaceRoot,
		"files":        result.Files,
		"file_count":   result.FileCount,
		"dir_count":    result.DirCount,
		"truncated":    result.Truncated,
		"skipped_dirs": result.SkippedDirs,
	})
}
