//go:build !js

// Design status endpoint — SP-140-6 §6b (TODO item 6.2).
//
// GET /api/design/status is the one read-only design endpoint (the spec's
// deliberate amendment to SP-140-3 §3f's zero-endpoint preference: findings/
// drift/feedback aggregates cannot be expressed by /api/files + /api/file
// without shipping a second validator to the browser, which would fork the
// truth). It composes the same pkg/design scanners the agent tools use
// (BuildDesignStatus), so the webui reads the same truth the agent reads.
//
// A workspace with no design/ returns {exists:false} with 200 (§6b: the
// health strip renders "no design tree", not an error). Writes nothing;
// Gate-1 (PrecheckFileAccess) is the agent-tool surface, not HTTP.
package webui

import (
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// handleAPIDesignStatus serves GET /api/design/status: the tree's validation
// tallies (capped findings), the two §5c drift rows with remedies, pending §4d
// feedback, and per-token reference counts (the SP-140-7 §7c alias-warning
// input). The code-ahead evidence set is nil on this path — an HTTP read has
// no ChangeTracker context, so that row reports synced/not-assessable (the
// honest tree-only answer, §5c).
func (ws *ReactWebServer) handleAPIDesignStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	status := design.BuildDesignStatus(workspaceRoot, nil)
	writeJSON(w, http.StatusOK, status)
}
