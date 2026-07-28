//go:build !js

// Package webui ... MCP server credential and OAuth management.
package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// ---------------------------------------------------------------------------
// GET/PUT/DELETE /api/settings/mcp/servers/{name}/credentials
// ---------------------------------------------------------------------------

// handleAPISettingsMCPServerCredentials dispatches GET/PUT/DELETE for
// /api/settings/mcp/servers/{name}/credentials.
func (ws *ReactWebServer) handleAPISettingsMCPServerCredentials(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleGetServerCredentials(w, r)
	case http.MethodPut:
		ws.handlePutServerCredentials(w, r)
	case http.MethodDelete:
		ws.handleDeleteServerCredentials(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// credentialStatusResponse represents the status of a single credential.
type credentialStatusResponse struct {
	Status   string `json:"status"` // "set" or "missing"
	HasValue bool   `json:"has_value"`
}

// getServerCredentialsResponse is the response for GET /api/settings/mcp/servers/{name}/credentials.
type getServerCredentialsResponse struct {
	Server      string                              `json:"server"`
	Credentials map[string]credentialStatusResponse `json:"credentials"`
}

// handleGetServerCredentials returns the credential status for a server.
// extractServerNameFromCredentialsPath extracts the server name from paths like
// /api/settings/mcp/servers/{name}/credentials. It strips the fixed /credentials
// suffix after extracting with extractPathSegment.
func extractServerNameFromCredentialsPath(path string) string {
	segment := extractPathSegment(path, "/api/settings/mcp/servers/")
	// Remove the /credentials suffix
	return strings.TrimSuffix(segment, "/credentials")
}

func (ws *ReactWebServer) handleGetServerCredentials(w http.ResponseWriter, r *http.Request) {
	name := extractServerNameFromCredentialsPath(r.URL.Path)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	server, exists := cfg.MCP.Servers[name]
	if !exists {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("MCP server %q not found", name))
		return
	}

	// Build credential status map
	credStatusMap := make(map[string]credentialStatusResponse)

	// Check Credentials map first
	if server.Credentials != nil {
		for envVarName, value := range server.Credentials {
			if mcp.IsSecretRef(value) {
				// Parse the placeholder to get the actual env var name
				_, actualEnvVarName, ok := mcp.ParseSecretRef(value)
				if !ok {
					// Invalid placeholder, mark as missing
					credStatusMap[envVarName] = credentialStatusResponse{
						Status:   "missing",
						HasValue: false,
					}
					continue
				}

				// Try to get from credential store
				key := mcp.CredentialKey(name, actualEnvVarName)
				credValue, _, err := credentials.GetFromActiveBackend(key)
				if err != nil || credValue == "" {
					// Fall back to OS environment
					credValue = os.Getenv(actualEnvVarName)
				}

				if credValue != "" {
					credStatusMap[envVarName] = credentialStatusResponse{
						Status:   "set",
						HasValue: true,
					}
				} else {
					credStatusMap[envVarName] = credentialStatusResponse{
						Status:   "missing",
						HasValue: false,
					}
				}
			}
		}
	}

	// Also check Env block for backward compatibility (credentials stored as secrets)
	if server.Env != nil {
		for envVarName, value := range server.Env {
			if mcp.IsSecretRef(value) {
				// Check if already added from Credentials map
				if _, exists := credStatusMap[envVarName]; exists {
					continue
				}

				// Parse the placeholder to get the actual env var name
				_, actualEnvVarName, ok := mcp.ParseSecretRef(value)
				if !ok {
					continue
				}

				// Try to get from credential store
				key := mcp.CredentialKey(name, actualEnvVarName)
				credValue, _, err := credentials.GetFromActiveBackend(key)
				if err != nil || credValue == "" {
					// Fall back to OS environment
					credValue = os.Getenv(actualEnvVarName)
				}

				if credValue != "" {
					credStatusMap[envVarName] = credentialStatusResponse{
						Status:   "set",
						HasValue: true,
					}
				} else {
					credStatusMap[envVarName] = credentialStatusResponse{
						Status:   "missing",
						HasValue: false,
					}
				}
			}
		}
	}

	response := getServerCredentialsResponse{
		Server:      name,
		Credentials: credStatusMap,
	}

	writeJSON(w, http.StatusOK, response)
}

// putServerCredentialsRequest is the request body for PUT /api/settings/mcp/servers/{name}/credentials.
type putServerCredentialsRequest struct {
	Credentials map[string]string `json:"credentials"`
}

// handlePutServerCredentials sets credentials for a server.
func (ws *ReactWebServer) handlePutServerCredentials(w http.ResponseWriter, r *http.Request) {
	name := extractServerNameFromCredentialsPath(r.URL.Path)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var req putServerCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if len(req.Credentials) == 0 {
		writeJSONError(w, http.StatusBadRequest, "credentials map cannot be empty")
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			return fmt.Errorf("MCP server %q not found", name)
		}
		server, exists := cfg.MCP.Servers[name]
		if !exists {
			return fmt.Errorf("MCP server %q not found", name)
		}

		// Process each credential with rollback on failure.
		// Track successfully written backend keys so they can be rolled back
		// if a subsequent write fails, preventing orphaned credentials.
		var writtenKeys []string
		for envVarName, plaintextValue := range req.Credentials {
			if plaintextValue == "" {
				continue // Skip empty values
			}
			if !isValidEnvVarName(envVarName) {
				return fmt.Errorf("invalid credential name %q: must match [A-Za-z_][A-Za-z0-9_]*", envVarName)
			}

			// Store the plaintext value in the credential backend
			key := mcp.CredentialKey(name, envVarName)
			if err := credentials.SetToActiveBackend(key, plaintextValue); err != nil {
				// Rollback: remove any credentials we already wrote
				for _, rollbackKey := range writtenKeys {
					if delErr := credentials.DeleteFromActiveBackend(rollbackKey); delErr != nil {
						ws.log().Error("failed to roll back MCP credential", slog.String("credential_key", rollbackKey), slog.Any("err", delErr))
					}
				}
				return fmt.Errorf("failed to store credential %s: %w", key, err)
			}
			writtenKeys = append(writtenKeys, key)

			// Set the server.Credentials entry to the placeholder
			if server.Credentials == nil {
				server.Credentials = make(map[string]string)
			}
			server.Credentials[envVarName] = mcp.SecretRef(name, envVarName)

			// Remove from Env if it exists there (migration)
			if server.Env != nil {
				delete(server.Env, envVarName)
			}
		}

		// Update the server config
		cfg.MCP.Servers[name] = server
		cfg.MCP.Enabled = true

		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSONError(w, http.StatusNotFound, err.Error())
		} else {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	// Return the updated credential status
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"server":  name,
	})
}

// deleteServerCredentialsRequest is the request body for DELETE /api/settings/mcp/servers/{name}/credentials.
type deleteServerCredentialsRequest struct {
	CredentialName string `json:"credential_name"`
}

// handleDeleteServerCredentials deletes a credential for a server.
func (ws *ReactWebServer) handleDeleteServerCredentials(w http.ResponseWriter, r *http.Request) {
	name := extractServerNameFromCredentialsPath(r.URL.Path)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var req deleteServerCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if req.CredentialName == "" {
		writeJSONError(w, http.StatusBadRequest, "credential_name is required")
		return
	}
	if !isValidEnvVarName(req.CredentialName) {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid credential name %q: must match [A-Za-z_][A-Za-z0-9_]*", req.CredentialName))
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			return fmt.Errorf("MCP server %q not found", name)
		}
		server, exists := cfg.MCP.Servers[name]
		if !exists {
			return fmt.Errorf("MCP server %q not found", name)
		}

		// Delete from credential store
		key := mcp.CredentialKey(name, req.CredentialName)
		if err := credentials.DeleteFromActiveBackend(key); err != nil {
			ws.log().Warn("failed to delete MCP credential", slog.String("credential_key", key), slog.Any("err", err))
			// Don't fail the request if delete fails - it might not exist
		}

		// Remove from server.Credentials
		if server.Credentials != nil {
			delete(server.Credentials, req.CredentialName)
		}
		// Also remove from server.Env (defense-in-depth for stale refs)
		if server.Env != nil {
			delete(server.Env, req.CredentialName)
		}

		// Update the server config
		cfg.MCP.Servers[name] = server

		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSONError(w, http.StatusNotFound, err.Error())
		} else {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":            true,
		"server":             name,
		"deleted_credential": req.CredentialName,
	})
}
