//go:build !js

package webui

import (
	"encoding/json"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// settings_api_partial_structs.go — the complex-struct partial-settings
// helpers (round-tripped via JSON) split out of the original
// settings_api_partial_settings.go. Pure move; signature contract is the
// same applyXxx(cfg, patch, knownKeys) error as the rest of the set.

// ---------------------------------------------------------------------------
// Complex structs (round-tripped via JSON)
// ---------------------------------------------------------------------------

func applyMCPSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["mcp"]; ok {
		knownKeys["mcp"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid mcp config: %w", err)
		}
		var mcpCfg mcp.MCPConfig
		if err := json.Unmarshal(raw, &mcpCfg); err != nil {
			return fmt.Errorf("invalid mcp config: %w", err)
		}
		truncateMCPConfig(&mcpCfg)
		cfg.MCP = mcpCfg
	}
	return nil
}

func applyCustomProvidersSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["custom_providers"]; ok {
		knownKeys["custom_providers"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid custom_providers config: %w", err)
		}
		var providers map[string]configuration.CustomProviderConfig
		if err := json.Unmarshal(raw, &providers); err != nil {
			return fmt.Errorf("invalid custom_providers config: %w", err)
		}
		for i, p := range providers {
			providers[i] = truncateCustomProvider(p)
		}
		cfg.CustomProviders = providers
	}
	return nil
}

func applyEmbeddingIndexSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["embedding_index"]; ok {
		knownKeys["embedding_index"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid embedding_index config: %w", err)
		}
		var ei configuration.EmbeddingIndexConfig
		if err := json.Unmarshal(raw, &ei); err != nil {
			return fmt.Errorf("invalid embedding_index config: %w", err)
		}
		for i, p := range ei.ExcludePaths {
			ei.ExcludePaths[i] = truncateString(p, maxSettingPathLength)
		}
		// Provider field removed — embedding provider is always the
		// bundled ONNX EmbeddingGemma-300M today.
		ei.IndexDir = truncateString(ei.IndexDir, maxSettingPathLength)
		cfg.EmbeddingIndex = &ei
	}
	return nil
}

func applyComputerUseSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["computer_use"]; ok {
		knownKeys["computer_use"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid computer_use config: %w", err)
		}
		var cu configuration.ComputerUseConfig
		if err := json.Unmarshal(raw, &cu); err != nil {
			return fmt.Errorf("invalid computer_use config: %w", err)
		}
		cu.AuditLogDir = truncateString(cu.AuditLogDir, maxSettingPathLength)
		for i, p := range cu.WorkspaceAllowlist {
			cu.WorkspaceAllowlist[i] = truncateString(p, maxSettingPathLength)
		}
		cfg.ComputerUse = &cu
	}
	return nil
}

func applyLanguageServerSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["language_servers"]; ok {
		knownKeys["language_servers"] = true
		if v == nil {
			cfg.LanguageServers = nil
		} else {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("invalid language_servers config: %w", err)
			}
			var servers []configuration.LanguageServerOverride
			if err := json.Unmarshal(raw, &servers); err != nil {
				return fmt.Errorf("invalid language_servers config: %w", err)
			}
			for i := range servers {
				servers[i].ID = truncateString(servers[i].ID, maxSettingNameLength)
				servers[i].Binary = truncateString(servers[i].Binary, maxSettingPathLength)
				servers[i].InstallHint = truncateString(servers[i].InstallHint, maxSettingDescriptionLength)
				for j := range servers[i].Args {
					servers[i].Args[j] = truncateString(servers[i].Args[j], maxSettingPathLength)
				}
				for j := range servers[i].LanguageIDs {
					servers[i].LanguageIDs[j] = truncateString(servers[i].LanguageIDs[j], maxSettingNameLength)
				}
			}
			cfg.LanguageServers = servers
		}
	}
	return nil
}

func applyPersistentContextSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["persistent_context"]; ok {
		knownKeys["persistent_context"] = true
		if v == nil {
			cfg.PersistentContext = nil
		} else {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("invalid persistent_context config: %w", err)
			}
			var pc configuration.PersistentContextConfig
			if err := json.Unmarshal(raw, &pc); err != nil {
				return fmt.Errorf("invalid persistent_context config: %w", err)
			}
			cfg.PersistentContext = &pc
		}
	}
	return nil
}

func applySkillsSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["skills"]; ok {
		knownKeys["skills"] = true
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid skills config: %w", err)
		}
		var skills map[string]configuration.Skill
		if err := json.Unmarshal(raw, &skills); err != nil {
			return fmt.Errorf("invalid skills config: %w", err)
		}
		for name, s := range skills {
			skills[name] = truncateSkill(s)
		}
		cfg.Skills = skills
	}
	return nil
}

// wakeupPatch is the field-level decode target for the "wakeup" section.
// Pointers distinguish "key present in the patch" from "key absent": only the
// fields the client actually sent are applied, so an unmentioned field keeps
// its current value instead of being zeroed. This is the regression behind the
// all-zeros wakeup block (Enabled:false, budgets 0) found in a real user config:
// a partial patch like {"wakeup":{"enabled":true}} used to unmarshal into a
// zero-valued WakeupConfig and wipe the budgets.
type wakeupPatch struct {
	Enabled              *bool `json:"enabled"`
	MaxTokensPerSession  *int  `json:"max_tokens_per_session"`
	MaxResumesPerSession *int  `json:"max_resumes_per_session"`
}

func applyWakeupSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["wakeup"]; ok {
		knownKeys["wakeup"] = true
		if v == nil {
			cfg.Wakeup = configuration.DefaultWakeupConfig()
			return nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid wakeup config: %w", err)
		}
		// Field-level merge: decode into typed pointers so only the keys
		// present in the patch are applied. Unknown keys are ignored (matching
		// the tolerant style of the surrounding appliers); a wrong JSON type
		// for a known key (e.g. enabled:"yes") or a non-object payload
		// (string/number/bool/array) fails the unmarshal.
		var wc wakeupPatch
		if err := json.Unmarshal(raw, &wc); err != nil {
			return fmt.Errorf("invalid wakeup config: %w", err)
		}
		// Validate budget values before mutating cfg so an error leaves the
		// config untouched (atomicity, asserted by the tests).
		if wc.MaxTokensPerSession != nil && *wc.MaxTokensPerSession < 0 {
			return fmt.Errorf("invalid wakeup config: max_tokens_per_session must be >= 0")
		}
		if wc.MaxResumesPerSession != nil && *wc.MaxResumesPerSession < 0 {
			return fmt.Errorf("invalid wakeup config: max_resumes_per_session must be >= 0")
		}
		if wc.Enabled != nil {
			cfg.Wakeup.Enabled = *wc.Enabled
		}
		if wc.MaxTokensPerSession != nil {
			cfg.Wakeup.MaxTokensPerSession = *wc.MaxTokensPerSession
		}
		if wc.MaxResumesPerSession != nil {
			cfg.Wakeup.MaxResumesPerSession = *wc.MaxResumesPerSession
		}
	}
	return nil
}

func applyCommandPoliciesSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["command_policies"]; ok {
		knownKeys["command_policies"] = true
		if v == nil {
			cfg.CommandPolicies = nil
			return nil
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid command_policies: %w", err)
		}
		var cp configuration.CommandPolicies
		if err := json.Unmarshal(raw, &cp); err != nil {
			return fmt.Errorf("invalid command_policies: %w", err)
		}
		cfg.CommandPolicies = &cp
	}
	return nil
}
