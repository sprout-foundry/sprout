package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/mcp"
)

// ---------------------------------------------------------------------------
// Method router — MCP settings
// ---------------------------------------------------------------------------

// handleAPISettingsMCP dispatches GET and PUT /api/settings/mcp.
func (ws *ReactWebServer) handleAPISettingsMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsMCPGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsMCPPut(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPISettingsMCPServers dispatches POST/PUT/DELETE /api/settings/mcp/servers/{name}.
func (ws *ReactWebServer) handleAPISettingsMCPServers(w http.ResponseWriter, r *http.Request) {
	// If the path ends with /credentials, delegate to the credentials handler
	if strings.HasSuffix(r.URL.Path, "/credentials") {
		ws.handleAPISettingsMCPServerCredentials(w, r)
		return
	}

	switch r.Method {
	case http.MethodPost:
		// POST without a name in URL means list-level create
		// The name is taken from the body.
		ws.handleAPISettingsMCPServersPost(w, r)
	case http.MethodPut:
		ws.handleAPISettingsMCPServersPut(w, r)
	case http.MethodDelete:
		ws.handleAPISettingsMCPServersDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// Helpers — MCP config with masked secrets
// ---------------------------------------------------------------------------

// mcpConfigResponse wraps MCPConfig for JSON serialization with masked env vars.
type mcpConfigResponse struct {
	Enabled      bool                           `json:"enabled"`
	Servers      map[string]mcpServerResponse    `json:"servers"`
	AutoStart    bool                           `json:"auto_start"`
	AutoDiscover bool                           `json:"auto_discover"`
	Timeout      time.Duration                  `json:"timeout"`
}

// mcpServerResponse wraps MCPServerConfig with masked env vars and credentials for the API response.
type mcpServerResponse struct {
	Name        string            `json:"name"`
	Type        string            `json:"type,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Credentials map[string]string `json:"credentials,omitempty"` // Credential env var names (masked values)
	WorkingDir  string            `json:"working_dir,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	AutoStart   bool              `json:"auto_start"`
	MaxRestarts int               `json:"max_restarts"`
}

// newMCPConfigResponse builds a response object with secret env var values masked.
func newMCPConfigResponse(cfg mcp.MCPConfig) mcpConfigResponse {
	servers := make(map[string]mcpServerResponse, len(cfg.Servers))
	for name, s := range cfg.Servers {
		servers[name] = mcpServerResponse{
			Name:        s.Name,
			Type:        s.Type,
			Command:     s.Command,
			Args:        s.Args,
			URL:         s.URL,
			Env:         mcp.MaskEnvVars(s.Env),
			Credentials: maskCredentials(s.Credentials),
			WorkingDir:  s.WorkingDir,
			Timeout:     s.Timeout,
			AutoStart:   s.AutoStart,
			MaxRestarts: s.MaxRestarts,
		}
	}
	return mcpConfigResponse{
		Enabled:      cfg.Enabled,
		Servers:      servers,
		AutoStart:    cfg.AutoStart,
		AutoDiscover: cfg.AutoDiscover,
		Timeout:      cfg.Timeout,
	}
}

// newMCPServerResponse builds a response for a single server with masked env vars.
func newMCPServerResponse(s mcp.MCPServerConfig) mcpServerResponse {
	return mcpServerResponse{
		Name:        s.Name,
		Type:        s.Type,
		Command:     s.Command,
		Args:        s.Args,
		URL:         s.URL,
		Env:         mcp.MaskEnvVars(s.Env),
		Credentials: maskCredentials(s.Credentials),
		WorkingDir:  s.WorkingDir,
		Timeout:     s.Timeout,
		AutoStart:   s.AutoStart,
		MaxRestarts: s.MaxRestarts,
	}
}

// maskCredentials masks credential values for safe display.
// Credential placeholders are shown as "{{stored}}".
// Raw (non-placeholder) values are masked via credentials.MaskValue.
func maskCredentials(creds map[string]string) map[string]string {
	if creds == nil {
		return nil
	}
	result := make(map[string]string, len(creds))
	for name, value := range creds {
		if mcp.IsSecretRef(value) {
			result[name] = "{{stored}}"
		} else if value != "" {
			result[name] = credentials.MaskValue(value)
		} else {
			result[name] = value
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// GET /api/settings/mcp
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	response := newMCPConfigResponse(cfg.MCP)
	writeJSON(w, http.StatusOK, response)
}

// ---------------------------------------------------------------------------
// PUT /api/settings/mcp
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPPut(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var incoming struct {
		Enabled      *bool  `json:"enabled"`
		AutoStart    *bool  `json:"auto_start"`
		AutoDiscover *bool  `json:"auto_discover"`
		Timeout      string `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if incoming.Enabled != nil {
			cfg.MCP.Enabled = *incoming.Enabled
		}
		if incoming.AutoStart != nil {
			cfg.MCP.AutoStart = *incoming.AutoStart
		}
		if incoming.AutoDiscover != nil {
			cfg.MCP.AutoDiscover = *incoming.AutoDiscover
		}
		if incoming.Timeout != "" {
			d, err := time.ParseDuration(incoming.Timeout)
			if err != nil {
				return fmt.Errorf("invalid timeout duration: %w", err)
			}
			cfg.MCP.Timeout = d
		}
		return nil
	}); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"mcp":     newMCPConfigResponse(updated.MCP),
	})
}

// ---------------------------------------------------------------------------
// POST /api/settings/mcp/servers
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPServersPost(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var server mcp.MCPServerConfig
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if server.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required")
		return
	}

	// Validate server config BEFORE doing any work to avoid unnecessary side effects.
	if err := validateMCPServer(server); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		if _, exists := cfg.MCP.Servers[server.Name]; exists {
			return fmt.Errorf("MCP server %q already exists (use PUT to update)", server.Name)
		}

		// Migrate secrets AFTER confirming the server doesn't exist, so that
		// no backend entries are orphaned if the existence check fails.
		if _, migrateErr := mcp.MigrateEnvSecretsFromServer(server.Name, &server); migrateErr != nil {
			return fmt.Errorf("failed to store server credentials: %w", migrateErr)
		}

		cfg.MCP.Servers[server.Name] = server
		cfg.MCP.Enabled = true
		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "failed to store server credentials") {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		} else {
			writeJSONError(w, http.StatusConflict, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"server":  newMCPServerResponse(server),
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/mcp/servers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPServersPut(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/mcp/servers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var server mcp.MCPServerConfig
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Ensure the name in the body matches the URL (and always have Name populated)
	server.Name = name

	// Validate server config BEFORE doing any work to avoid unnecessary side effects.
	if err := validateMCPServer(server); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		if _, exists := cfg.MCP.Servers[name]; !exists {
			return fmt.Errorf("MCP server %q not found (use POST to create)", name)
		}

		// Migrate secrets AFTER confirming the server exists, so that
		// no backend entries are orphaned if the existence check fails.
		if _, migrateErr := mcp.MigrateEnvSecretsFromServer(server.Name, &server); migrateErr != nil {
			return fmt.Errorf("failed to store server credentials: %w", migrateErr)
		}

		cfg.MCP.Servers[name] = server
		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "failed to store server credentials") {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		} else {
			writeJSONError(w, http.StatusNotFound, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"server":  newMCPServerResponse(server),
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/mcp/servers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPServersDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/mcp/servers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "server name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
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

		// Clean up stored secrets for this server from both Env and Credentials
		cleanupMCPServerSecrets(name, server)

		delete(cfg.MCP.Servers, name)
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": name,
	})
}

// cleanupMCPServerSecrets removes credential store entries for env vars that are
// stored as secrets for a given MCP server. It checks both the Env and Credentials
// maps to handle all migration states (refs may live in either or both locations).
func cleanupMCPServerSecrets(serverName string, server mcp.MCPServerConfig) {
	for envVarName, value := range server.Env {
		if mcp.IsSecretRef(value) {
			key := mcp.CredentialKey(serverName, envVarName)
			if err := credentials.DeleteFromActiveBackend(key); err != nil {
				log.Printf("[mcp] Failed to delete credential %s: %v", key, err)
			}
		}
	}
	if server.Credentials != nil {
		for envVarName, value := range server.Credentials {
			if mcp.IsSecretRef(value) {
				_, actualEnvVarName, ok := mcp.ParseSecretRef(value)
				if !ok {
					actualEnvVarName = envVarName
				}
				key := mcp.CredentialKey(serverName, actualEnvVarName)
				if err := credentials.DeleteFromActiveBackend(key); err != nil {
					log.Printf("[mcp] Failed to delete credential %s: %v", key, err)
				}
			}
		}
	}
}

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
	Status    string `json:"status"`     // "set" or "missing"
	HasValue  bool   `json:"has_value"`
}

// getServerCredentialsResponse is the response for GET /api/settings/mcp/servers/{name}/credentials.
type getServerCredentialsResponse struct {
	Server      string                          `json:"server"`
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
						log.Printf("[mcp] Failed to rollback credential %s: %v", rollbackKey, delErr)
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
			log.Printf("[mcp] Failed to delete credential %s: %v", key, err)
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

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// isValidEnvVarName returns true if name looks like a valid environment variable
// name (e.g. "MY_VAR_1"). This is used to validate credential key names in the
// credential management API to prevent storing under nonsensical keys.
func isValidEnvVarName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for i, c := range name {
		if i == 0 {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_') {
				return false
			}
		} else {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
				return false
			}
		}
	}
	return true
}

func validateMCPServer(s mcp.MCPServerConfig) error {
	if s.Name == "" {
		return fmt.Errorf("server name cannot be empty")
	}
	if strings.ContainsAny(s.Name, "/\\") {
		return fmt.Errorf("server name cannot contain slashes")
	}
	if strings.Contains(s.Name, "..") {
		return fmt.Errorf("server name cannot contain '..'")
	}
	serverType := strings.TrimSpace(strings.ToLower(s.Type))
	if serverType == "http" {
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("URL is required for HTTP servers")
		}
	} else {
		// stdio (default)
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("command is required for stdio servers")
		}
	}
	if s.MaxRestarts < 0 {
		return fmt.Errorf("max_restarts must be non-negative")
	}
	if s.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}
	return nil
}
