package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

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
// GET /api/settings/mcp
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsMCPGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, cfg.MCP)
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
		"mcp":     updated.MCP,
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

	// Validate server config
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
		"server":  server,
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
		cfg.MCP.Servers[name] = server
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"server":  server,
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
		if _, exists := cfg.MCP.Servers[name]; !exists {
			return fmt.Errorf("MCP server %q not found", name)
		}
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

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateMCPServer(s mcp.MCPServerConfig) error {
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
