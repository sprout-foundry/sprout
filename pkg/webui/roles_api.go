//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// handleAPIRoles handles all role CRUD endpoints under /api/roles and /api/roles/{name}.
func (ws *ReactWebServer) handleAPIRoles(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}
	rm := cm.GetRoleManager()

	// Extract the role name from the path (after /api/roles/)
	name := extractPathSegment(r.URL.Path, "/api/roles/")
	name = strings.ToLower(name)

	switch r.Method {
	case http.MethodGet:
		if name == "" {
			ws.handleRolesList(rm, w, r)
		} else {
			ws.handleRolesGet(rm, w, r, name)
		}
	case http.MethodPost:
		ws.handleRolesCreate(rm, w, r)
	case http.MethodPut:
		if name == "" {
			writeJSONError(w, http.StatusBadRequest, "role name is required in path")
		} else {
			ws.handleRolesUpdate(rm, w, r, name)
		}
	case http.MethodDelete:
		if name == "" {
			writeJSONError(w, http.StatusBadRequest, "role name is required in path")
		} else {
			ws.handleRolesDelete(rm, w, r, name)
		}
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRolesList returns metadata for all roles.
func (ws *ReactWebServer) handleRolesList(rm *configuration.RoleManager, w http.ResponseWriter, r *http.Request) {
	roles, err := rm.List()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list roles: "+err.Error())
		return
	}
	if roles == nil {
		roles = []configuration.RoleMeta{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"roles": roles,
	})
}

// handleRolesGet returns a specific role configuration by name.
func (ws *ReactWebServer) handleRolesGet(rm *configuration.RoleManager, w http.ResponseWriter, r *http.Request, name string) {
	role, err := rm.Resolve(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSONError(w, http.StatusNotFound, "role not found: "+name)
		} else {
			writeJSONError(w, http.StatusInternalServerError, "failed to get role: "+err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, role)
}

// handleRolesCreate saves a new role configuration.
func (ws *ReactWebServer) handleRolesCreate(rm *configuration.RoleManager, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var cfg configuration.RoleConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if cfg.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "role name is required")
		return
	}

	// Normalize the name to lowercase for consistent lookups
	cfg.Name = strings.TrimSpace(strings.ToLower(cfg.Name))

	if rm.Exists(cfg.Name) {
		writeJSONError(w, http.StatusBadRequest, "role '"+cfg.Name+"' already exists")
		return
	}

	if err := rm.Save(cfg, "workspace"); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create role: "+err.Error())
		return
	}

	role, err := rm.Resolve(cfg.Name)
	if err != nil {
		// Save succeeded but read-back failed; return what we received
		writeJSON(w, http.StatusCreated, cfg)
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

// handleRolesUpdate modifies an existing role configuration.
func (ws *ReactWebServer) handleRolesUpdate(rm *configuration.RoleManager, w http.ResponseWriter, r *http.Request, name string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var cfg configuration.RoleConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	cfg.Name = name

	if !rm.Exists(name) {
		writeJSONError(w, http.StatusNotFound, "role not found: "+name)
		return
	}

	if err := rm.Save(cfg, "workspace"); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to update role: "+err.Error())
		return
	}

	role, err := rm.Resolve(name)
	if err != nil {
		// Save succeeded but read-back failed; return what we received
		writeJSON(w, http.StatusOK, cfg)
		return
	}
	writeJSON(w, http.StatusOK, role)
}

// handleRolesDelete removes a role configuration.
func (ws *ReactWebServer) handleRolesDelete(rm *configuration.RoleManager, w http.ResponseWriter, r *http.Request, name string) {
	if !rm.Exists(name) {
		writeJSONError(w, http.StatusNotFound, "role not found: "+name)
		return
	}

	if err := rm.Delete(name); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete role: "+err.Error())
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}
