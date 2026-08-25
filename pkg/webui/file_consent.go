//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (ws *ReactWebServer) handleAPIFileConsent(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	fileConsents := ws.getFileConsentManagerForRequest(r)
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	var req struct {
		Path      string `json:"path"`
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	if operation != "read" && operation != "write" {
		writeJSONErr(w, http.StatusBadRequest, "operation_must_be_read_or_write", "operation must be read or write")
		return
	}

	canonicalPath, err := canonicalizePath(req.Path, workspaceRoot, operation == "write")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_file_path", fmt.Sprintf("Invalid file path: %v", err))
		return
	}

	if isWithinWorkspace(canonicalPath, workspaceRoot) || isAppConfigPath(canonicalPath) {
		writeJSONErr(w, http.StatusBadRequest, "path_does_not_require_external_consent", "Path does not require external consent")
		return
	}

	token, expiresAt, err := fileConsents.issue(canonicalPath, operation, defaultConsentTTL)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_create_consent_token", fmt.Sprintf("Failed to create consent token: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":      token,
		"path":       canonicalPath,
		"operation":  operation,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

func (ws *ReactWebServer) writeExternalPathConsentRequired(w http.ResponseWriter, canonicalPath, operation string) {
	writeJSON(w, http.StatusForbidden, map[string]interface{}{
		"error":     "external path access requires explicit user consent",
		"code":      "external_path_consent_required",
		"path":      canonicalPath,
		"operation": operation,
	})
}
