package platformauth

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// MCPServerName is the MCP server `sprout login` adds to sprout's global MCP
// config: the platform's own MCP server, reached with the sign-in token, so
// sprout can list the person's projects and add, follow and manage their
// tasks on their behalf.
const MCPServerName = "sprout-foundry"

// mcpAuthHeader is the header the token is sent in.
const mcpAuthHeader = "Authorization"

// MCPURL is the platform's MCP endpoint.
func MCPURL(platformURL string) string {
	return strings.TrimRight(platformURL, "/") + "/mcp"
}

// ConfigureMCP adds (or refreshes) the platform's MCP server in sprout's
// global MCP config, with the token kept in the credential store and only a
// placeholder in the config file. A server of that name the person added
// themselves is replaced: the sign-in is the newer intent.
func ConfigureMCP(platformURL, token string) error {
	if err := credentials.SetToActiveBackend(mcp.HeaderCredentialKey(MCPServerName, mcpAuthHeader), "Bearer "+token); err != nil {
		return fmt.Errorf("storing the MCP credential: %w", err)
	}
	cfg, err := mcp.LoadGlobalMCPConfigFile()
	if err != nil {
		return fmt.Errorf("reading the MCP config: %w", err)
	}
	server := mcp.MCPServerConfig{
		Name:      MCPServerName,
		Type:      "http",
		URL:       MCPURL(platformURL),
		Headers:   map[string]string{mcpAuthHeader: mcp.HeaderSecretRef(MCPServerName, mcpAuthHeader)},
		AutoStart: true,
	}
	server.SetTrustUntrustedContent(true)
	if err := cfg.AddServer(server); err != nil {
		return err
	}
	if err := mcp.SaveMCPConfig(&cfg); err != nil {
		return fmt.Errorf("saving the MCP config: %w", err)
	}
	return nil
}

// RemoveMCP takes the platform's MCP server out of sprout's MCP config and
// forgets its credential. It reports whether a server was configured.
func RemoveMCP() (bool, error) {
	_ = credentials.DeleteFromActiveBackend(mcp.HeaderCredentialKey(MCPServerName, mcpAuthHeader))
	cfg, err := mcp.LoadGlobalMCPConfigFile()
	if err != nil {
		return false, fmt.Errorf("reading the MCP config: %w", err)
	}
	if _, ok := cfg.Servers[MCPServerName]; !ok {
		return false, nil
	}
	cfg.RemoveServer(MCPServerName)
	if err := mcp.SaveMCPConfig(&cfg); err != nil {
		return true, fmt.Errorf("saving the MCP config: %w", err)
	}
	return true, nil
}
