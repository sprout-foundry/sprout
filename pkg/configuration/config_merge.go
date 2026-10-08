package configuration

import (
	"encoding/json"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// MergeConfig merges two configs, with override taking precedence over base.
// The override config typically contains only changed fields (deltas).
// Returns a new config without modifying either input.
func MergeConfig(base, override *Config) *Config {
	if base == nil {
		return cloneConfig(override)
	}
	if override == nil {
		return cloneConfig(base)
	}

	result := cloneConfig(base)
	result.mergeExplicitKeys(override)

	// Override simple string fields if non-empty
	if override.LastUsedProvider != "" {
		result.LastUsedProvider = override.LastUsedProvider
	}

	// Merge ProviderModels - override takes precedence
	if len(override.ProviderModels) > 0 {
		if result.ProviderModels == nil {
			result.ProviderModels = make(map[string]string)
		}
		for k, v := range override.ProviderModels {
			result.ProviderModels[k] = v
		}
	}

	// Override slices if non-empty
	if len(override.ProviderPriority) > 0 {
		result.ProviderPriority = override.ProviderPriority
	}

	// Merge MCP config
	if override.overrides("mcp.enabled", override.MCP.Enabled) {
		result.MCP.Enabled = override.MCP.Enabled
	}
	if override.MCP.Timeout > 0 {
		result.MCP.Timeout = override.MCP.Timeout
	}
	if override.MCP.Servers != nil {
		if result.MCP.Servers == nil {
			result.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		for k, v := range override.MCP.Servers {
			result.MCP.Servers[k] = v
		}
	}

	// Merge Preferences
	if len(override.Preferences) > 0 {
		if result.Preferences == nil {
			result.Preferences = make(map[string]interface{})
		}
		for k, v := range override.Preferences {
			result.Preferences[k] = v
		}
	}

	// Override simple bool/int/string fields
	if override.ResourceDirectory != "" {
		result.ResourceDirectory = override.ResourceDirectory
	}
	if override.ReasoningEffort != "" {
		result.ReasoningEffort = override.ReasoningEffort
	}
	if override.OutputVerbosity != "" {
		result.OutputVerbosity = override.OutputVerbosity
	}
	if override.overrides("show_tool_invocations", override.ShowToolInvocations) {
		result.ShowToolInvocations = override.ShowToolInvocations
	}
	// Language is a single-value selector — a non-empty override wins.
	if override.Language != "" {
		result.Language = override.Language
	}
	// The language guard is on by default; the opt-out is a
	// boolean with the same explicit-key semantics as disable_thinking, so
	// a layer that named disable_language_guard wins with either value —
	// including false, which re-enables the guard over a broader disable.
	if override.overrides("disable_language_guard", override.DisableLanguageGuard) {
		result.DisableLanguageGuard = override.DisableLanguageGuard
	}
	if override.overrides("disable_thinking", override.DisableThinking) {
		result.DisableThinking = override.DisableThinking
	}
	if override.SystemPromptText != "" {
		result.SystemPromptText = override.SystemPromptText
	}
	if override.overrides("skip_prompt", override.SkipPrompt) {
		result.SkipPrompt = override.SkipPrompt
	}

	// SP-058: RiskProfile is a single-value selector; non-empty override wins.
	if override.RiskProfile != "" {
		result.RiskProfile = override.RiskProfile
	}
	if len(override.RiskProfiles) > 0 {
		if result.RiskProfiles == nil {
			result.RiskProfiles = make(map[string]AutoApproveRules, len(override.RiskProfiles))
		}
		for k, v := range override.RiskProfiles {
			result.RiskProfiles[k] = v
		}
	}
	// Merge Roles with field-wise precedence. Unlike
	// RiskProfiles (which replaces a named profile wholesale), a role
	// named in both layers keeps every field the override left empty
	// from the base layer — so a project-level roles.commit {model: …}
	// keeps the global provider. Roles named in only one layer pass
	// through. There is no unsetting: an override field set to empty
	// cannot clear a base value, the same semantics as RiskProfiles.
	if len(override.Roles) > 0 {
		if result.Roles == nil {
			result.Roles = make(map[string]RoleConfig, len(override.Roles))
		}
		for name, overrideRole := range override.Roles {
			merged := result.Roles[name] // zero when the base layer lacks the role
			if overrideRole.Provider != "" {
				merged.Provider = overrideRole.Provider
			}
			if overrideRole.Model != "" {
				merged.Model = overrideRole.Model
			}
			result.Roles[name] = merged
		}
	}
	// ContextMode is a single-value selector — non-empty override wins.
	if override.ContextMode != "" {
		result.ContextMode = override.ContextMode
	}
	// Union-merge ApprovedShellCommands so workspace entries stack on global.
	if len(override.ApprovedShellCommands) > 0 {
		seen := make(map[string]struct{}, len(result.ApprovedShellCommands)+len(override.ApprovedShellCommands))
		merged := make([]string, 0, len(result.ApprovedShellCommands)+len(override.ApprovedShellCommands))
		for _, cmd := range result.ApprovedShellCommands {
			if _, ok := seen[cmd]; ok {
				continue
			}
			seen[cmd] = struct{}{}
			merged = append(merged, cmd)
		}
		for _, cmd := range override.ApprovedShellCommands {
			if _, ok := seen[cmd]; ok {
				continue
			}
			seen[cmd] = struct{}{}
			merged = append(merged, cmd)
		}
		result.ApprovedShellCommands = merged
	}
	// Same union-merge for ApprovedShellCommandPatterns.
	if len(override.ApprovedShellCommandPatterns) > 0 {
		seen := make(map[string]struct{}, len(result.ApprovedShellCommandPatterns)+len(override.ApprovedShellCommandPatterns))
		merged := make([]string, 0, len(result.ApprovedShellCommandPatterns)+len(override.ApprovedShellCommandPatterns))
		for _, p := range result.ApprovedShellCommandPatterns {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			merged = append(merged, p)
		}
		for _, p := range override.ApprovedShellCommandPatterns {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			merged = append(merged, p)
		}
		result.ApprovedShellCommandPatterns = merged
	}

	// Merge APITimeouts
	if override.APITimeouts != nil {
		if result.APITimeouts == nil {
			result.APITimeouts = &APITimeoutConfig{}
		}
		if override.APITimeouts.ConnectionTimeoutSec > 0 {
			result.APITimeouts.ConnectionTimeoutSec = override.APITimeouts.ConnectionTimeoutSec
		}
		if override.APITimeouts.FirstChunkTimeoutSec > 0 {
			result.APITimeouts.FirstChunkTimeoutSec = override.APITimeouts.FirstChunkTimeoutSec
		}
		if override.APITimeouts.ChunkTimeoutSec > 0 {
			result.APITimeouts.ChunkTimeoutSec = override.APITimeouts.ChunkTimeoutSec
		}
		if override.APITimeouts.OverallTimeoutSec > 0 {
			result.APITimeouts.OverallTimeoutSec = override.APITimeouts.OverallTimeoutSec
		}
		if override.APITimeouts.CommitMessageTimeoutSec > 0 {
			result.APITimeouts.CommitMessageTimeoutSec = override.APITimeouts.CommitMessageTimeoutSec
		}
	}

	// Merge CustomProviders
	if len(override.CustomProviders) > 0 {
		if result.CustomProviders == nil {
			result.CustomProviders = make(map[string]CustomProviderConfig)
		}
		for k, v := range override.CustomProviders {
			result.CustomProviders[k] = v
		}
	}

	// Override CommandHistoryByPath and HistoryIndexByPath
	if len(override.CommandHistoryByPath) > 0 {
		result.CommandHistoryByPath = override.CommandHistoryByPath
	}
	if len(override.HistoryIndexByPath) > 0 {
		result.HistoryIndexByPath = override.HistoryIndexByPath
	}

	// Override HistoryScope
	if override.HistoryScope != "" {
		result.HistoryScope = override.HistoryScope
	}

	// Override subagent settings
	if override.SubagentProvider != "" {
		result.SubagentProvider = override.SubagentProvider
	}
	if override.SubagentModel != "" {
		result.SubagentModel = override.SubagentModel
	}
	if override.SubagentMaxParallel > 0 {
		result.SubagentMaxParallel = override.SubagentMaxParallel
	}
	if override.SubagentParallelEnabled != nil {
		result.SubagentParallelEnabled = override.SubagentParallelEnabled
	}
	if override.SubagentMaxDepth > 0 {
		result.SubagentMaxDepth = override.SubagentMaxDepth
	}

	// Merge SubagentTypes
	if len(override.SubagentTypes) > 0 {
		if result.SubagentTypes == nil {
			result.SubagentTypes = make(map[string]SubagentType)
		}
		for k, v := range override.SubagentTypes {
			result.SubagentTypes[k] = v
		}
	}

	// Override commit provider/model
	if override.CommitProvider != "" {
		result.CommitProvider = override.CommitProvider
	}
	if override.CommitModel != "" {
		result.CommitModel = override.CommitModel
	}

	// Override review provider/model
	if override.ReviewProvider != "" {
		result.ReviewProvider = override.ReviewProvider
	}
	if override.ReviewModel != "" {
		result.ReviewModel = override.ReviewModel
	}

	// Merge Skills
	if len(override.Skills) > 0 {
		if result.Skills == nil {
			result.Skills = make(map[string]Skill)
		}
		for k, v := range override.Skills {
			if v.Metadata == nil {
				v.Metadata = make(map[string]string)
			}
			if _, has := v.Metadata["source"]; !has {
				v.Metadata["source"] = "user"
			}
			result.Skills[k] = v
		}
	}

	// Override zsh settings
	if override.overrides("enable_zsh_command_detection", override.EnableZshCommandDetection) {
		result.EnableZshCommandDetection = override.EnableZshCommandDetection
	}
	if override.overrides("auto_execute_detected_commands", override.AutoExecuteDetectedCommands) {
		result.AutoExecuteDetectedCommands = override.AutoExecuteDetectedCommands
	}

	// Merge Shell configuration (SP-049 Phase 2)
	if len(override.Shell.UserSafePatterns) > 0 {
		result.Shell.UserSafePatterns = append([]ShellPattern{}, override.Shell.UserSafePatterns...)
	}
	if len(override.Shell.UserDangerousPatterns) > 0 {
		result.Shell.UserDangerousPatterns = append([]ShellPattern{}, override.Shell.UserDangerousPatterns...)
	}
	if override.Shell.WorkspaceOverlay.Mode != "" {
		result.Shell.WorkspaceOverlay = override.Shell.WorkspaceOverlay
	}

	// Merge Training configuration.
	if override.overrides("training.enabled", override.Training.Enabled) {
		result.Training.Enabled = true
	}
	if override.Training.Endpoint != "" {
		result.Training.Endpoint = override.Training.Endpoint
	}
	if len(override.Training.ExcludePaths) > 0 {
		result.Training.ExcludePaths = mergeStringSlices(result.Training.ExcludePaths, override.Training.ExcludePaths)
	}

	// Merge Verification configuration. The
	// feature is off by default; the enable flag carries explicit-key
	// semantics so a narrower layer can disable a broader layer's
	// enable, the repair-attempt limit and the total repair-rounds cap
	// follow the non-zero-wins convention of the other numeric caps, and
	// the explicit build/test commands follow the non-empty-wins
	// convention of the other string fields (a narrower layer's command
	// beats a broader one; a silent layer keeps it).
	if v := override.Verification; v != nil &&
		(override.overrides("verification.enabled", v.Enabled) || v.RepairAttempts > 0 ||
			v.TotalRepairRounds > 0 || v.BuildCommand != "" || v.TestCommand != "" ||
			override.overrides("verification.require_test", v.RequireTest)) {
		if result.Verification == nil {
			result.Verification = &VerificationConfig{}
		}
		if override.overrides("verification.enabled", v.Enabled) {
			result.Verification.Enabled = v.Enabled
		}
		if override.overrides("verification.require_test", v.RequireTest) {
			result.Verification.RequireTest = v.RequireTest
		}
		if v.RepairAttempts > 0 {
			result.Verification.RepairAttempts = v.RepairAttempts
		}
		if v.TotalRepairRounds > 0 {
			result.Verification.TotalRepairRounds = v.TotalRepairRounds
		}
		if v.BuildCommand != "" {
			result.Verification.BuildCommand = v.BuildCommand
		}
		if v.TestCommand != "" {
			result.Verification.TestCommand = v.TestCommand
		}
	}

	// Merge Quality configuration. The feature is off by default; the
	// enable flag carries explicit-key semantics so a narrower layer can
	// disable a broader layer's enable, and the explicit formatter/linter
	// commands follow the non-empty-wins convention of the other string
	// fields (a narrower layer's command beats a broader one; a silent
	// layer keeps it).
	if q := override.Quality; q != nil &&
		(override.overrides("quality.enabled", q.Enabled) ||
			q.FormatCommand != "" || q.LintCommand != "") {
		if result.Quality == nil {
			result.Quality = &QualityConfig{}
		}
		if override.overrides("quality.enabled", q.Enabled) {
			result.Quality.Enabled = q.Enabled
		}
		if q.FormatCommand != "" {
			result.Quality.FormatCommand = q.FormatCommand
		}
		if q.LintCommand != "" {
			result.Quality.LintCommand = q.LintCommand
		}
	}

	// Merge RepetitionGuard configuration. The guard is on by default; the
	// enable flag carries explicit-key semantics so a narrower layer can
	// disable a broader layer's enable (and a bare *bool false is honored
	// because the pointer is present). The thresholds follow the
	// non-zero-wins convention.
	if rg := override.RepetitionGuard; rg != nil &&
		(rg.Enabled != nil || rg.MinRepetitions > 0 || rg.MaxLineChars > 0) {
		if result.RepetitionGuard == nil {
			result.RepetitionGuard = &RepetitionGuardConfig{}
		}
		if rg.Enabled != nil {
			result.RepetitionGuard.Enabled = boolPtr(*rg.Enabled)
		}
		if rg.MinRepetitions > 0 {
			result.RepetitionGuard.MinRepetitions = rg.MinRepetitions
		}
		if rg.MaxLineChars > 0 {
			result.RepetitionGuard.MaxLineChars = rg.MaxLineChars
		}
	}

	return result
}

// cloneConfig creates a deep copy of a Config
func cloneConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	// Unexported — the roundtrip above drops it, same as SubagentTypes below.
	out.explicitKeys = cfg.copyExplicitKeys()
	// SubagentTypes is tagged json:"-" so the roundtrip strips it; copy directly.
	if len(cfg.SubagentTypes) > 0 {
		out.SubagentTypes = make(map[string]SubagentType, len(cfg.SubagentTypes))
		for id, st := range cfg.SubagentTypes {
			copied := st
			copied.AllowedTools = append([]string{}, st.AllowedTools...)
			copied.Aliases = append([]string{}, st.Aliases...)
			copied.Capabilities = append([]string{}, st.Capabilities...)
			copied.CanSpawnNonDelegatable = append([]string{}, st.CanSpawnNonDelegatable...)
			if st.AutoApproveRules != nil {
				rules := *st.AutoApproveRules
				rules.LowRiskOps = append([]string{}, rules.LowRiskOps...)
				rules.MediumRiskOps = append([]string{}, rules.MediumRiskOps...)
				rules.HighRiskNever = append([]string{}, rules.HighRiskNever...)
				copied.AutoApproveRules = &rules
			}
			out.SubagentTypes[id] = copied
		}
	} else {
		out.SubagentTypes = defaultSubagentTypes()
	}
	return &out
}

// mergeStringSlices combines two string slices, removing duplicates.
func mergeStringSlices(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	result := make([]string, 0, len(base)+len(extra))
	for _, s := range base {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range extra {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
