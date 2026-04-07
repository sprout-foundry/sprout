package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

// mcpServerResponse wraps MCPServerConfig with masked env vars for the API response.
type mcpServerResponse struct {
	Name        string            `json:"name"`
	Type        string            `json:"type,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
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
		WorkingDir:  s.WorkingDir,
		Timeout:     s.Timeout,
		AutoStart:   s.AutoStart,
		MaxRestarts: s.MaxRestarts,
	}
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

	// Validate server config BEFORE migrating secrets to avoid orphaning
	// credentials if validation fails.
	if err := validateMCPServer(server); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Migrate any plaintext secrets in the env var block to the credential store
	if _, err := mcp.MigrateEnvSecretsFromServer(server.Name, &server); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to store server credentials: %v", err))
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		if _, exists := cfg.MCP.Servers[server.Name]; exists {
			return fmt.Errorf("MCP server %q already exists (use PUT to update)", server.Name)
		}
		cfg.MCP.Servers[server.Name] = server
		cfg.MCP.Enabled = true
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
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

	// Validate server config BEFORE migrating secrets to avoid orphaning
	// credentials if validation fails.
	if err := validateMCPServer(server); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Migrate any plaintext secrets in the env var block to the credential store
	if _, err := mcp.MigrateEnvSecretsFromServer(server.Name, &server); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to store server credentials: %v", err))
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		if _, exists := cfg.MCP.Servers[name]; !exists {
			return fmt.Errorf("MCP server %q not found (use POST to create)", name)
		}
		cfg.MCP.Servers[name] = server
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
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

		// Clean up stored secrets for this server
		cleanupMCPServerSecrets(name, server.Env)

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
// stored as secrets for a given MCP server.
func cleanupMCPServerSecrets(serverName string, env map[string]string) {
	if env == nil {
		return
	}
	for envVarName, value := range env {
		if mcp.IsSecretRef(value) {
			key := mcp.CredentialKey(serverName, envVarName)
			if err := credentials.DeleteFromActiveBackend(key); err != nil {
				log.Printf("[mcp] Failed to delete credential %s: %v", key, err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

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
