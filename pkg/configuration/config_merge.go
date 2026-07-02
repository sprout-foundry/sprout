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
	if override.MCP.Enabled {
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
	if override.DisableThinking {
		result.DisableThinking = override.DisableThinking
	}
	if override.SystemPromptText != "" {
		result.SystemPromptText = override.SystemPromptText
	}
	if override.SkipPrompt {
		result.SkipPrompt = override.SkipPrompt
	}

	// SP-058: RiskProfile is a single-value selector; non-empty
	// override wins. RiskProfiles is a map of named overrides; we
	// merge per-key so a workspace can override just one profile
	// without wiping out user-defined profiles from the global
	// config.
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
	// ApprovedShellCommands: union the two lists (override entries are
	// additive to base). De-dupe so a workspace config that re-lists a
	// command already in the global config doesn't grow the file.
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
	// ApprovedShellCommandPatterns: same union-merge as ApprovedShellCommands
	// so patterns defined at the workspace layer stack on top of the global
	// layer rather than silently overwriting it.
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

	// Merge DismissedPrompts
	if len(override.DismissedPrompts) > 0 {
		if result.DismissedPrompts == nil {
			result.DismissedPrompts = make(map[string]bool)
		}
		for k, v := range override.DismissedPrompts {
			result.DismissedPrompts[k] = v
		}
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

	// Merge EmbeddingIndex
	if override.EmbeddingIndex != nil {
		if result.EmbeddingIndex == nil {
			result.EmbeddingIndex = &EmbeddingIndexConfig{}
		}
		if override.EmbeddingIndex.Enabled {
			result.EmbeddingIndex.Enabled = override.EmbeddingIndex.Enabled
		}
		if override.EmbeddingIndex.IndexDir != "" {
			result.EmbeddingIndex.IndexDir = override.EmbeddingIndex.IndexDir
		}
		if override.EmbeddingIndex.SimilarityThreshold > 0 {
			result.EmbeddingIndex.SimilarityThreshold = override.EmbeddingIndex.SimilarityThreshold
		}
		if override.EmbeddingIndex.MaxResults > 0 {
			result.EmbeddingIndex.MaxResults = override.EmbeddingIndex.MaxResults
		}
		if override.EmbeddingIndex.AutoIndex {
			result.EmbeddingIndex.AutoIndex = override.EmbeddingIndex.AutoIndex
		}
		if len(override.EmbeddingIndex.ExcludePaths) > 0 {
			result.EmbeddingIndex.ExcludePaths = append([]string{}, override.EmbeddingIndex.ExcludePaths...)
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

	// Override PDF OCR settings
	if override.PDFOCREnabled {
		result.PDFOCREnabled = override.PDFOCREnabled
	}
	if override.PDFOCRProvider != "" {
		result.PDFOCRProvider = override.PDFOCRProvider
	}
	if override.PDFOCRModel != "" {
		result.PDFOCRModel = override.PDFOCRModel
	}
	if override.VisionFallbackToOCR {
		result.VisionFallbackToOCR = override.VisionFallbackToOCR
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
	if override.EnableZshCommandDetection {
		result.EnableZshCommandDetection = override.EnableZshCommandDetection
	}
	if override.AutoExecuteDetectedCommands {
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
	// SubagentTypes is intentionally tagged json:"-" (personas are catalog-fixed
	// and not persisted to disk). The JSON roundtrip strips it, so we copy it
	// directly from the source — preserving any in-memory mutations (e.g. test
	// fixtures that inject custom personas, workflow-automation overrides).
	// If the source map is empty, fall back to the catalog defaults so callers
	// never see a nil/empty SubagentTypes from a freshly loaded config.
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
