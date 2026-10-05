//go:build !js

// Starter API — SP-153 §153b (TODO 153.6).
//
// Two endpoints back the web UI's new-project flow (the dialog wiring
// lands in 153.7):
//
//   - GET  /api/starters             — the embedded, versioned starter
//     catalogue (starters.List, plus per-starter tree facts).
//   - POST /api/starters/instantiate — populate a fresh project directory
//     with a starter (starters.Instantiate), writing the tree plus the
//     project's .sprout/starter.json.
//
// The handlers are stateless with respect to client context: the client
// sends a real absolute target path (the new-project dialog's file
// browser is rooted at the daemon root and returns absolute paths), so
// neither a workspace root nor a client ID is consulted.
package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/starters"
)

// starterListItem is one entry of GET /api/starters. The identity fields
// mirror the startermanifest on-disk contract (starter.id / starter.
// version), plus the two tree facts the dialog displays: how many
// project-content files the starter carries (files, from
// starters.FileCount) and whether its descriptor — the manifest that
// instantiation writes — is present (has_manifest, from
// starters.Manifest; always true for the embedded catalogue, computed
// honestly rather than assumed).
type starterListItem struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Files       int    `json:"files"`
	HasManifest bool   `json:"has_manifest"`
}

// starterListResponse is the body of GET /api/starters.
type starterListResponse struct {
	Starters []starterListItem `json:"starters"`
}

// instantiateResponse is the 200 body of POST /api/starters/instantiate:
// where the project was created, which starter populated it, how many
// project-content files the tree carries, and the manifest that was
// written to .sprout/starter.json (the single source of build/test/dev/
// preview commands for the project's later consumers).
type instantiateResponse struct {
	Root     string                           `json:"root"`
	Starter  string                           `json:"starter"`
	Files    int                              `json:"files"`
	Manifest *startermanifest.StarterManifest `json:"manifest"`
}

// handleAPIStartersList handles GET /api/starters: the embedded starter
// catalogue (SP-153 §153b, TODO 153.6).
func (ws *ReactWebServer) handleAPIStartersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	list, err := starters.ListForUsers()
	if err != nil {
		// A malformed embedded tree is a build bug in this repository,
		// not a client error: 500, not 400.
		writeJSONErr(w, http.StatusInternalServerError, "starter_list_failed", fmt.Sprintf("list embedded starters: %v", err))
		return
	}

	items := make([]starterListItem, 0, len(list))
	for _, s := range list {
		files, ferr := starters.FileCount(s.ID)
		if ferr != nil {
			// The catalogue just listed this starter, so a FileCount
			// failure is the same build-bug class.
			writeJSONErr(w, http.StatusInternalServerError, "starter_list_failed", ferr.Error())
			return
		}
		_, merr := starters.Manifest(s.ID)
		items = append(items, starterListItem{
			ID:          s.ID,
			Version:     s.Version,
			Files:       files,
			HasManifest: merr == nil,
		})
	}

	writeJSON(w, http.StatusOK, starterListResponse{Starters: items})
}

// errPathNotAbsolute is returned by resolveStarterTargetPath when the
// (expanded) target path is not absolute. The instantiate contract
// requires a real absolute path: relative input is rejected with 400
// rather than anchored at the daemon process's CWD — a place the client
// cannot predict.
var errPathNotAbsolute = errors.New("path must be an absolute path")

// resolveStarterTargetPath canonicalizes the instantiate target path,
// following the path-safety convention of the sibling file endpoints
// (canonicalizePath with forWrite semantics): $HOME/${HOME} and tilde
// expansion, a Clean+Abs normalization that collapses `..` traversal
// segments, and symlink resolution on the nearest existing parent, so
// the write lands where the canonical path points rather than through a
// symlinked ancestor.
//
// The one addition over canonicalizePath: the path must be absolute
// after expansion, and that is enforced here (errPathNotAbsolute).
// Relative input would otherwise be silently resolved against the
// process CWD, which is exactly the silent-misanchor the sibling
// endpoints' convention exists to prevent.
func resolveStarterTargetPath(raw string) (string, error) {
	expanded := expandHomeVar(raw)
	if hasTildePrefix(expanded) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if expanded == "~" {
			expanded = home
		} else {
			expanded = filepath.Join(home, expanded[2:])
		}
	}
	if !filepath.IsAbs(expanded) {
		return "", errPathNotAbsolute
	}
	canonical, err := canonicalizePath(expanded, "", true)
	if err != nil {
		return "", fmt.Errorf("canonicalize target path: %w", err)
	}
	return canonical, nil
}

// validateStarterName checks the optional project name in the instantiate
// body. The server does not use it to build paths (the dialog displays
// it and owns it client-side); it only ensures the value is a single
// safe name rather than a path, so a client cannot smuggle traversal
// into the field.
func validateStarterName(name string) error {
	if name == "" {
		return nil // absent is allowed.
	}
	if name == "." || name == ".." {
		return errors.New("name must not be \".\" or \"..\"")
	}
	if strings.ContainsAny(name, "/\\") {
		return errors.New("name must not contain a path separator")
	}
	return nil
}

// handleAPIStartersInstantiate handles POST /api/starters/instantiate
// (SP-153 §153b, TODO 153.6): populate a fresh project directory with
// the embedded starter named in the body.
//
// Body: {"starter": "<id>", "path": "<absolute target dir>", "name":
// "<optional project name>"}.
//
// Error mapping (the writeJSONErr code+message convention the sibling
// endpoints use):
//
//	invalid body / missing field / relative path / unsafe name → 400
//	unknown starter id                                         → 404
//	target directory non-empty (refused before any write)      → 400
//
// The 404 for an unknown starter mirrors the CLI half (153.4): the
// client can see what is available via GET /api/starters.
func (ws *ReactWebServer) handleAPIStartersInstantiate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)

	var req struct {
		Starter string `json:"starter"`
		Path    string `json:"path"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	req.Starter = strings.TrimSpace(req.Starter)
	req.Path = strings.TrimSpace(req.Path)
	req.Name = strings.TrimSpace(req.Name)

	if req.Starter == "" {
		writeJSONErr(w, http.StatusBadRequest, "starter_required", "starter is required")
		return
	}
	if req.Path == "" {
		writeJSONErr(w, http.StatusBadRequest, "path_required", "path is required")
		return
	}
	if err := validateStarterName(req.Name); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_name", err.Error())
		return
	}

	target, err := resolveStarterTargetPath(req.Path)
	if err != nil {
		code := "invalid_path"
		if errors.Is(err, errPathNotAbsolute) {
			code = "path_must_be_absolute"
		}
		writeJSONErr(w, http.StatusBadRequest, code, err.Error())
		return
	}

	// Contain the instantiate target to the daemon root (SP-153, TODO fix.8).
	// Without this, a client could point `path` anywhere the daemon process
	// can write and drop a full project tree there. Mirror
	// handleAPIWorkspaceBrowse: resolve the daemon root's symlinks and reject
	// a canonical target that is neither the root nor strictly under it.
	daemonRoot := ws.GetDaemonRoot()
	resolvedDaemonRoot := daemonRoot
	if evaled, err := filepath.EvalSymlinks(daemonRoot); err == nil {
		resolvedDaemonRoot = evaled
	}
	if target != resolvedDaemonRoot && !isWithinWorkspace(target, resolvedDaemonRoot) {
		writeJSONErr(w, http.StatusForbidden, "target_outside_daemon_root",
			"target directory is outside the daemon root")
		return
	}

	if err := starters.Instantiate(req.Starter, target); err != nil {
		switch {
		case errors.Is(err, starters.ErrUnknownStarter):
			writeJSONErr(w, http.StatusNotFound, "unknown_starter",
				fmt.Sprintf("unknown starter %q: no embedded starter with that id (see GET /api/starters)", req.Starter))
		case errors.Is(err, starters.ErrInvalidStarterID):
			writeJSONErr(w, http.StatusBadRequest, "invalid_starter_id", err.Error())
		case errors.Is(err, starters.ErrNonEmptyDestination):
			// Nothing was written; the directory is left exactly as it was.
			writeJSONErr(w, http.StatusBadRequest, "destination_not_empty", err.Error())
		default:
			writeJSONErr(w, http.StatusBadRequest, "instantiate_failed", err.Error())
		}
		return
	}

	// The starter was just instantiated, so both lookups must succeed;
	// a failure here is a build-bug class (500), not a client error.
	manifest, merr := starters.Manifest(req.Starter)
	if merr != nil {
		writeJSONErr(w, http.StatusInternalServerError, "starter_manifest_failed", merr.Error())
		return
	}
	files, ferr := starters.FileCount(req.Starter)
	if ferr != nil {
		writeJSONErr(w, http.StatusInternalServerError, "starter_list_failed", ferr.Error())
		return
	}

	writeJSON(w, http.StatusOK, instantiateResponse{
		Root:     target,
		Starter:  req.Starter,
		Files:    files,
		Manifest: manifest,
	})
}
