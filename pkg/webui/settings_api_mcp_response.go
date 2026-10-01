//go:build !js

package webui

// settings_api_mcp_response.go — MCP settings response shapes, split out
// of settings_api_mcp.go. mcpConfigResponse / mcpServerResponse are the
// JSON-facing views of MCP config with secrets masked; newMCPConfigResponse,
// newMCPServerResponse and maskCredentials build them (credential
// placeholders show as "{{stored}}", raw values go through
// credentials.MaskValue).
import (
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// ---------------------------------------------------------------------------
// Helpers — MCP config with masked secrets
// ---------------------------------------------------------------------------

// mcpConfigResponse wraps MCPConfig for JSON serialization with masked env vars.
type mcpConfigResponse struct {
	Enabled      bool                         `json:"enabled"`
	Servers      map[string]mcpServerResponse `json:"servers"`
	AutoStart    bool                         `json:"auto_start"`
	AutoDiscover bool                         `json:"auto_discover"`
	Timeout      time.Duration                `json:"timeout"`
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
