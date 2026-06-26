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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	var req struct {
		Path      string `json:"path"`
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	if operation != "read" && operation != "write" {
		http.Error(w, "operation must be read or write", http.StatusBadRequest)
		return
	}

	canonicalPath, err := canonicalizePath(req.Path, workspaceRoot, operation == "write")
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid file path: %v", err), http.StatusBadRequest)
		return
	}

	if isWithinWorkspace(canonicalPath, workspaceRoot) || isAppConfigPath(canonicalPath) {
		http.Error(w, "Path does not require external consent", http.StatusBadRequest)
		return
	}

	token, expiresAt, err := fileConsents.issue(canonicalPath, operation, defaultConsentTTL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create consent token: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      token,
		"path":       canonicalPath,
		"operation":  operation,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

func (ws *ReactWebServer) writeExternalPathConsentRequired(w http.ResponseWriter, canonicalPath, operation string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":     "external path access requires explicit user consent",
		"code":      "external_path_consent_required",
		"path":      canonicalPath,
		"operation": operation,
	})
}
