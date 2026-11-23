//go:build !js

package webui

import (
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/filediscovery"
)

// handleAPIFileIndex serves GET /api/file-index — the project folder's whole
// quick-open index in ONE response. The command palette previously crawled
// the tree one /api/browse request per directory (hundreds of serial
// round-trips on real repos); this endpoint walks the tree server-side with
// the same ignore rules and caps and returns every row at once.
//
// Rows are project-folder-relative paths: the response carries no absolute paths,
// so a cached index can never leak rows across a project folder switch.
func (ws *ReactWebServer) handleAPIFileIndex(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectFolder := ws.getProjectFolderForRequest(r)
	if projectFolder == "" {
		writeJSONErr(w, http.StatusBadRequest, "project_folder_not_selected", "No project folder selected")
		return
	}
	// Same home-project-folder hardening as /api/browse and /api/files (SP-130):
	// walking $HOME raises macOS TCC prompts and is what the project folder gate
	// exists to prevent.
	if isHomeProjectFolder(projectFolder) && !hasHomeProjectFolderConsent() {
		writeJSONErr(w, http.StatusForbidden, "project_folder_not_selected",
			"Project folder is the home directory and no consent was granted; select a project folder first")
		return
	}

	result := filediscovery.BuildFileIndex(projectFolder, filediscovery.DefaultIndexLimits())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":        "success",
		"project_folder": projectFolder,
		"files":          result.Files,
		"file_count":     result.FileCount,
		"dir_count":      result.DirCount,
		"truncated":      result.Truncated,
		"skipped_dirs":   result.SkippedDirs,
	})
}
