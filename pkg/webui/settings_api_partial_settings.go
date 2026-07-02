//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// This file breaks the original 500+ line applyPartialSettings monolith into
// per-domain helpers. Each helper owns one slice of the configuration surface:
// agent prompt/behavior, paths/context, risk/safety, subagent routing,
// provider routing, pdf-ocr, shell-detection, api timeouts, version, and the
// complex struct sections (mcp, custom_providers, embedding_index,
// computer_use, language_servers, persistent_context, security_policy,
// skills). The orchestrator in settings_api_put.go simply iterates them.
//
// Each helper signature:
//
//	applyXxx(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error
//
// The helper mutates cfg in place, marks consumed keys via knownKeys, and
// returns an error if validation fails. The orchestrator collects any patch
// keys that were NOT marked known and returns them as the "unknown" slice.

// ---------------------------------------------------------------------------
// Agent prompt + behavior
// ---------------------------------------------------------------------------

func applyAgentBehaviorSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["reasoning_effort"]; ok {
		knownKeys["reasoning_effort"] = true
		s, _ := v.(string)
		s = truncateString(s, maxSettingEnumLength)
		if err := validateReasoningEffort(s); err != nil {
			return fmt.Errorf("validate reasoning_effort: %w", err)
		}
		cfg.ReasoningEffort = s
	}
	if v, ok := patch["system_prompt_text"]; ok {
		knownKeys["system_prompt_text"] = true
		s, _ := v.(string)
		cfg.SystemPromptText = truncateString(s, maxSettingPromptLength)
	}
	if v, ok := patch["skip_prompt"]; ok {
		knownKeys["skip_prompt"] = true
		cfg.SkipPrompt, _ = v.(bool)
	}
	if v, ok := patch["output_verbosity"]; ok {
		knownKeys["output_verbosity"] = true
		s, _ := v.(string)
		s = strings.ToLower(strings.TrimSpace(truncateString(s, maxSettingEnumLength)))
		switch s {
		case "", "compact", "default", "verbose":
			cfg.OutputVerbosity = s
		default:
			return fmt.Errorf("validate output_verbosity: must be 'compact', 'default', or 'verbose' (got %q)", s)
		}
	}
	if v, ok := patch["disable_thinking"]; ok {
		knownKeys["disable_thinking"] = true
		cfg.DisableThinking, _ = v.(bool)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Paths + context window
// ---------------------------------------------------------------------------

func applyPathsAndContextSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["resource_directory"]; ok {
		knownKeys["resource_directory"] = true
		s, _ := v.(string)
		cfg.ResourceDirectory = truncateString(s, maxSettingPathLength)
	}
	if v, ok := patch["history_scope"]; ok {
		knownKeys["history_scope"] = true
		s, _ := v.(string)
		s = truncateString(s, maxSettingEnumLength)
		if err := validateHistoryScope(s); err != nil {
			return fmt.Errorf("validate history_scope: %w", err)
		}
		cfg.HistoryScope = s
	}
	if v, ok := patch["max_context_tokens"]; ok {
		knownKeys["max_context_tokens"] = true
		if v == nil {
			cfg.MaxContextTokens = nil
		} else {
			n, ok2 := asInt(v)
			if !ok2 || n < 0 {
				return fmt.Errorf("max_context_tokens must be a non-negative integer (0 = no limit)")
			}
			if n == 0 {
				cfg.MaxContextTokens = nil
			} else if n < 1024 {
				return fmt.Errorf("max_context_tokens must be at least 1024 when set (got %d)", n)
			} else {
				cfg.MaxContextTokens = &n
			}
		}
	}
	if v, ok := patch["ea_mode"]; ok {
		knownKeys["ea_mode"] = true
		s, _ := v.(string)
		s = strings.ToLower(strings.TrimSpace(truncateString(s, maxSettingEnumLength)))
		switch s {
		case "", "interactive", "queue":
			cfg.EAMode = s
		default:
			return fmt.Errorf("validate ea_mode: must be 'interactive' or 'queue' (got %q)", s)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Risk profile + safety gates
// ---------------------------------------------------------------------------

func applyRiskAndSafetySettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["risk_profile"]; ok {
		knownKeys["risk_profile"] = true
		s, _ := v.(string)
		// Allow empty (= unset → resolves to "default") and any
		// built-in profile name. User-defined names in
		// cfg.RiskProfiles are also accepted. Reject names that
		// match neither so a typo in the dropdown doesn't silently
		// fall back to default.
		if s != "" && !configuration.IsValidRiskProfile(s) {
			if _, ok := cfg.RiskProfiles[s]; !ok {
				return fmt.Errorf("validate risk_profile: unknown profile %q (built-in: readonly, cautious, default, permissive, unrestricted; or define your own in risk_profiles)", s)
			}
		}
		cfg.RiskProfile = s
	}
	if v, ok := patch["risk_profiles"]; ok {
		knownKeys["risk_profiles"] = true
		// Accept a map[name]AutoApproveRules. Round-trip via JSON so
		// we get type-safe decoding without depending on map[string]any
		// gymnastics for the nested struct. nil clears the override
		// map and falls back to baked-in defaults for all profiles.
		if v == nil {
			cfg.RiskProfiles = nil
		} else {
			raw, mErr := json.Marshal(v)
			if mErr != nil {
				return fmt.Errorf("validate risk_profiles: encode incoming value: %w", mErr)
			}
			var decoded map[string]configuration.AutoApproveRules
			if uErr := json.Unmarshal(raw, &decoded); uErr != nil {
				return fmt.Errorf("validate risk_profiles: %w", uErr)
			}
			cfg.RiskProfiles = decoded
		}
	}
	if v, ok := patch["self_review_gate_mode"]; ok {
		knownKeys["self_review_gate_mode"] = true
		s, _ := v.(string)
		s = truncateString(s, maxSettingEnumLength)
		if err := validateSelfReviewGateMode(s); err != nil {
			return fmt.Errorf("validate self_review_gate_mode: %w", err)
		}
		cfg.SelfReviewGateMode = s
	}
	if v, ok := patch["approved_shell_commands"]; ok {
		knownKeys["approved_shell_commands"] = true
		if arr, ok := v.([]interface{}); ok {
			out := make([]string, 0, len(arr))
			seen := make(map[string]struct{}, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					trimmed := strings.TrimSpace(truncateString(s, maxSettingPathLength))
					if trimmed == "" {
						continue
					}
					if _, dup := seen[trimmed]; dup {
						continue
					}
					seen[trimmed] = struct{}{}
					out = append(out, trimmed)
				}
			}
			cfg.ApprovedShellCommands = out
		} else if v == nil {
			cfg.ApprovedShellCommands = nil
		}
	}
	if v, ok := patch["security_policy"]; ok {
		knownKeys["security_policy"] = true
		if v == nil {
			cfg.SecurityPolicy = nil
		} else {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("invalid security_policy config: %w", err)
			}
			var sp configuration.SecurityPolicy
			if err := json.Unmarshal(raw, &sp); err != nil {
				return fmt.Errorf("invalid security_policy config: %w", err)
			}
			cfg.SecurityPolicy = &sp
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Subagent routing
// ---------------------------------------------------------------------------

func applySubagentSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["subagent_provider"]; ok {
		knownKeys["subagent_provider"] = true
		s, _ := v.(string)
		cfg.SubagentProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["subagent_model"]; ok {
		knownKeys["subagent_model"] = true
		s, _ := v.(string)
		cfg.SubagentModel = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["subagent_max_parallel"]; ok {
		knownKeys["subagent_max_parallel"] = true
		n, ok2 := asInt(v)
		if ok2 && n >= 0 {
			cfg.SubagentMaxParallel = n
		}
	}
	if v, ok := patch["subagent_parallel_enabled"]; ok {
		knownKeys["subagent_parallel_enabled"] = true
		b, ok2 := v.(bool)
		if ok2 {
			cfg.SubagentParallelEnabled = &b
		}
	}
	if v, ok := patch["default_subagent_persona"]; ok {
		knownKeys["default_subagent_persona"] = true
		s, _ := v.(string)
		s = strings.TrimSpace(truncateString(s, maxSettingNameLength))
		// Empty string clears the override (falls back to "general").
		// A non-empty value must reference a known persona; otherwise reject
		// rather than silently fail at spawn time.
		if s != "" && cfg.GetSubagentType(s) == nil {
			return fmt.Errorf("default_subagent_persona %q is not a known persona ID or alias", s)
		}
		cfg.DefaultSubagentPersona = s
	}
	if v, ok := patch["disabled_personas"]; ok {
		knownKeys["disabled_personas"] = true
		// Accept either []string or []interface{} (JSON unmarshals to the latter).
		var ids []string
		switch list := v.(type) {
		case []string:
			ids = list
		case []interface{}:
			for _, item := range list {
				if s, ok := item.(string); ok {
					ids = append(ids, s)
				}
			}
		case nil:
			ids = nil
		default:
			return fmt.Errorf("disabled_personas must be a list of persona IDs")
		}
		// Validate each entry resolves to a known persona. Unknown IDs would
		// silently no-op the disable, which is a quiet bug.
		var cleaned []string
		for _, id := range ids {
			trimmed := strings.TrimSpace(truncateString(id, maxSettingNameLength))
			if trimmed == "" {
				continue
			}
			if cfg.GetSubagentType(trimmed) == nil && !cfg.IsPersonaDisabled(trimmed) {
				// Allow re-listing an already-disabled persona (so the list is
				// stable across PUTs even after a catalog change removes one).
				return fmt.Errorf("disabled_personas: %q is not a known persona ID or alias", trimmed)
			}
			cleaned = append(cleaned, trimmed)
		}
		cfg.DisabledPersonas = cleaned
	}
	if v, ok := patch["subagent_max_depth"]; ok {
		knownKeys["subagent_max_depth"] = true
		n, ok2 := asInt(v)
		if ok2 && n >= 0 && n <= 32 {
			cfg.SubagentMaxDepth = n
		}
	}
	// SubagentTypes — personas are catalog-fixed. Older clients (and
	// round-trip GET→PUT flows like "copy global to workspace") may still
	// include this field; we accept-and-ignore so existing payloads don't
	// 400. Use 'disabled_personas' to hide a persona or 'default_subagent_persona'
	// to redirect default spawns.
	if _, ok := patch["subagent_types"]; ok {
		knownKeys["subagent_types"] = true
		// Intentionally no-op: the catalog is the source of truth.
	}
	return nil
}

// ---------------------------------------------------------------------------
// Provider routing (commit / review / per-provider model defaults)
// ---------------------------------------------------------------------------

func applyProviderRoutingSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["commit_provider"]; ok {
		knownKeys["commit_provider"] = true
		s, _ := v.(string)
		cfg.CommitProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["commit_model"]; ok {
		knownKeys["commit_model"] = true
		s, _ := v.(string)
		cfg.CommitModel = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["review_provider"]; ok {
		knownKeys["review_provider"] = true
		s, _ := v.(string)
		cfg.ReviewProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["review_model"]; ok {
		knownKeys["review_model"] = true
		s, _ := v.(string)
		cfg.ReviewModel = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["provider_models"]; ok {
		knownKeys["provider_models"] = true
		if m, ok := v.(map[string]interface{}); ok {
			pm := make(map[string]string, len(m))
			for k, val := range m {
				s, _ := val.(string)
				pm[truncateString(k, maxSettingNameLength)] = truncateString(s, maxSettingNameLength)
			}
			cfg.ProviderModels = pm
		}
	}
	if v, ok := patch["provider_priority"]; ok {
		knownKeys["provider_priority"] = true
		if arr, ok := v.([]interface{}); ok {
			pp := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					pp = append(pp, truncateString(s, maxSettingNameLength))
				}
			}
			cfg.ProviderPriority = pp
		}
	}
	if v, ok := patch["last_used_provider"]; ok {
		knownKeys["last_used_provider"] = true
		s, _ := v.(string)
		cfg.LastUsedProvider = truncateString(s, maxSettingNameLength)
	}
	return nil
}

// ---------------------------------------------------------------------------
// PDF OCR
// ---------------------------------------------------------------------------

func applyPDFOCRSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["pdf_ocr_enabled"]; ok {
		knownKeys["pdf_ocr_enabled"] = true
		cfg.PDFOCREnabled, _ = v.(bool)
	}
	if v, ok := patch["pdf_ocr_provider"]; ok {
		knownKeys["pdf_ocr_provider"] = true
		s, _ := v.(string)
		cfg.PDFOCRProvider = truncateString(s, maxSettingNameLength)
	}
	if v, ok := patch["pdf_ocr_model"]; ok {
		knownKeys["pdf_ocr_model"] = true
		s, _ := v.(string)
		cfg.PDFOCRModel = truncateString(s, maxSettingNameLength)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Shell command detection
// ---------------------------------------------------------------------------

func applyShellDetectionSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["enable_zsh_command_detection"]; ok {
		knownKeys["enable_zsh_command_detection"] = true
		cfg.EnableZshCommandDetection, _ = v.(bool)
	}
	if v, ok := patch["auto_execute_detected_commands"]; ok {
		knownKeys["auto_execute_detected_commands"] = true
		cfg.AutoExecuteDetectedCommands, _ = v.(bool)
	}
	return nil
}

// ---------------------------------------------------------------------------
// API timeouts
// ---------------------------------------------------------------------------

func applyAPITimeoutsSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if at, ok := patch["api_timeouts"]; ok {
		knownKeys["api_timeouts"] = true
		if atMap, ok := at.(map[string]interface{}); ok {
			if existing := cfg.APITimeouts; existing == nil {
				cfg.APITimeouts = &configuration.APITimeoutConfig{}
			}
			if v2, ok2 := atMap["connection_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.connection_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate connection_timeout_sec: %w", err)
				}
				cfg.APITimeouts.ConnectionTimeoutSec = n
			}
			if v2, ok2 := atMap["first_chunk_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.first_chunk_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate first_chunk_timeout_sec: %w", err)
				}
				cfg.APITimeouts.FirstChunkTimeoutSec = n
			}
			if v2, ok2 := atMap["chunk_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.chunk_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate chunk_timeout_sec: %w", err)
				}
				cfg.APITimeouts.ChunkTimeoutSec = n
			}
			if v2, ok2 := atMap["overall_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.overall_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate overall_timeout_sec: %w", err)
				}
				cfg.APITimeouts.OverallTimeoutSec = n
			}
			if v2, ok2 := atMap["commit_message_timeout_sec"]; ok2 {
				n, ok3 := asInt(v2)
				if !ok3 {
					return fmt.Errorf("api_timeouts.commit_message_timeout_sec must be a positive integer")
				}
				if err := validateAPITimeout(n); err != nil {
					return fmt.Errorf("validate commit_message_timeout_sec: %w", err)
				}
				cfg.APITimeouts.CommitMessageTimeoutSec = n
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Version bookkeeping
// ---------------------------------------------------------------------------

func applyVersionSettings(cfg *configuration.Config, patch map[string]interface{}, knownKeys map[string]bool) error {
	if v, ok := patch["version"]; ok {
		knownKeys["version"] = true
		s, _ := v.(string)
		cfg.Version = truncateString(s, maxSettingGenericLength)
	}
	return nil
}

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

// partialSettingsApplier is the ordered list of per-domain helpers invoked by
// applyPartialSettings. Order is not significant (each helper owns a disjoint
// set of keys) but reading it top-to-bottom roughly tracks the settings UI.
var partialSettingsAppliers = []func(*configuration.Config, map[string]interface{}, map[string]bool) error{
	applyAgentBehaviorSettings,
	applyPathsAndContextSettings,
	applyRiskAndSafetySettings,
	applySubagentSettings,
	applyProviderRoutingSettings,
	applyPDFOCRSettings,
	applyShellDetectionSettings,
	applyAPITimeoutsSettings,
	applyVersionSettings,
	applyMCPSettings,
	applyCustomProvidersSettings,
	applyEmbeddingIndexSettings,
	applyComputerUseSettings,
	applyLanguageServerSettings,
	applyPersistentContextSettings,
	applySkillsSettings,
}