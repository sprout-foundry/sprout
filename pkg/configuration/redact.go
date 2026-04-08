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

	// Deep-copy slices and maps to prevent shared-reference mutation
	if cfg.ProviderModels != nil {
		redacted.ProviderModels = make(map[string]string, len(cfg.ProviderModels))
		for k, v := range cfg.ProviderModels {
			redacted.ProviderModels[k] = v
		}
	}
	if cfg.ProviderPriority != nil {
		redacted.ProviderPriority = make([]string, len(cfg.ProviderPriority))
		copy(redacted.ProviderPriority, cfg.ProviderPriority)
	}
	if cfg.CustomProviders != nil {
		redacted.CustomProviders = make(map[string]CustomProviderConfig, len(cfg.CustomProviders))
		for k, v := range cfg.CustomProviders {
			redacted.CustomProviders[k] = v
		}
	}
	if cfg.Preferences != nil {
		redacted.Preferences = make(map[string]interface{}, len(cfg.Preferences))
		for k, v := range cfg.Preferences {
			redacted.Preferences[k] = v
		}
	}
	if cfg.DismissedPrompts != nil {
		redacted.DismissedPrompts = make(map[string]bool, len(cfg.DismissedPrompts))
		for k, v := range cfg.DismissedPrompts {
			redacted.DismissedPrompts[k] = v
		}
	}

	// Deep-copy APITimeouts (pointer field)
	if cfg.APITimeouts != nil {
		timeoutCopy := *cfg.APITimeouts
		redacted.APITimeouts = &timeoutCopy
	}

	// Deep-copy SubagentTypes (map of structs with slice fields)
	if cfg.SubagentTypes != nil {
		redacted.SubagentTypes = make(map[string]SubagentType, len(cfg.SubagentTypes))
		for k, v := range cfg.SubagentTypes {
			// Deep copy the struct's slice fields
			subagentCopy := v
			if v.AllowedTools != nil {
				subagentCopy.AllowedTools = make([]string, len(v.AllowedTools))
				copy(subagentCopy.AllowedTools, v.AllowedTools)
			}
			if v.Aliases != nil {
				subagentCopy.Aliases = make([]string, len(v.Aliases))
				copy(subagentCopy.Aliases, v.Aliases)
			}
			redacted.SubagentTypes[k] = subagentCopy
		}
	}

	// Deep-copy Skills (map of structs with map fields)
	if cfg.Skills != nil {
		redacted.Skills = make(map[string]Skill, len(cfg.Skills))
		for k, v := range cfg.Skills {
			// Deep copy the struct's map field
			skillCopy := v
			if v.Metadata != nil {
				skillCopy.Metadata = make(map[string]string, len(v.Metadata))
				for mk, mv := range v.Metadata {
					skillCopy.Metadata[mk] = mv
				}
			}
			redacted.Skills[k] = skillCopy
		}
	}

	return redacted
}