package mcp

import (
	"encoding/json"

	"github.com/alantheprice/ledit/pkg/credentials"
)

// RedactServerConfig returns a copy of server config with credentials redacted.
// The Credentials map values (which are credential reference placeholders like
// "{{credential:mcp/server/ENVVAR}}") are kept as-is since they are safe indirect
// references, not actual secrets. The Env map is redacted for sensitive keys.
func RedactServerConfig(server MCPServerConfig) MCPServerConfig {
	redacted := server

	// Redact the Credentials map (keep placeholder refs as-is, mask actual values)
	if server.Credentials != nil {
		redacted.Credentials = credentials.RedactMap(server.Credentials)
	}

	// Redact the Env map for sensitive keys
	if server.Env != nil {
		redacted.Env = credentials.RedactEnvMap(server.Env)
	}

	return redacted
}

// RedactMCPConfig returns a copy of the MCP config with all server credentials redacted.
// This is used for diagnostic output, config exports, and any other display paths
// where credential values should not be exposed.
func RedactMCPConfig(config MCPConfig) MCPConfig {
	redacted := config
	redacted.Servers = make(map[string]MCPServerConfig, len(config.Servers))

	for name, server := range config.Servers {
		redacted.Servers[name] = RedactServerConfig(server)
	}

	return redacted
}

// MarshalJSONWithRedaction marshals the MCP config to JSON with all credentials redacted.
// This is a convenience function that combines RedactMCPConfig with json.MarshalIndent.
func MarshalJSONWithRedaction(config MCPConfig) ([]byte, error) {
	redacted := RedactMCPConfig(config)
	return json.MarshalIndent(redacted, "", "  ")
}
