package configuration

import (
	"github.com/alantheprice/ledit/pkg/mcp"
)

// RedactConfig returns a copy of the configuration with all credential values redacted.
// The MCP server configs have their env vars and credentials maps redacted.
// This should be used for any display/export/diagnostic output where the config
// is shown to the user or logged.
func RedactConfig(cfg *Config) Config {
	if cfg == nil {
		return Config{}
	}

	redacted := *cfg
	redacted.MCP = mcp.RedactMCPConfig(cfg.MCP)
	return redacted
}