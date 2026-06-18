package configuration

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// GetSubagentProvider returns the configured provider for subagents
// If not explicitly set, falls back to the last used provider
func (c *Config) GetSubagentProvider() string {
	if c.SubagentProvider != "" {
		return c.SubagentProvider
	}
	// Fall back to last used provider
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	// Fall back to first priority provider
	if len(c.ProviderPriority) > 0 {
		return c.ProviderPriority[0]
	}
	return "ollama-local" // Ultimate fallback
}

// GetSubagentModel returns the configured model for subagents
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetSubagentModel() string {
	if c.SubagentModel != "" {
		return c.SubagentModel
	}
	// Use the provider for subagents
	provider := c.GetSubagentProvider()
	return c.GetModelForProvider(provider)
}

// SetSubagentProvider sets the provider for subagents
func (c *Config) SetSubagentProvider(provider string) {
	c.SubagentProvider = provider
}

// SetSubagentModel sets the model for subagents
func (c *Config) SetSubagentModel(model string) {
	c.SubagentModel = model
}

// IsPersonaDisabled reports whether the given persona ID has been disabled
// by the user (via /persona <id> disable). The canonical ID after alias
// resolution is matched; a disabled persona is returned as nil by
// GetSubagentType and filtered from GetAvailablePersonaIDs.
func (c *Config) IsPersonaDisabled(id string) bool {
	if c == nil {
		return false
	}
	needle := normalizePersonaID(id)
	for _, disabled := range c.DisabledPersonas {
		if normalizePersonaID(disabled) == needle {
			return true
		}
	}
	return false
}

// SetPersonaDisabled adds or removes a persona ID from DisabledPersonas.
// Idempotent; aliases are normalized to the canonical form before storage.
func (c *Config) SetPersonaDisabled(id string, disabled bool) {
	if c == nil {
		return
	}
	needle := normalizePersonaID(id)
	filtered := c.DisabledPersonas[:0:0]
	already := false
	for _, existing := range c.DisabledPersonas {
		if normalizePersonaID(existing) == needle {
			already = true
			if !disabled {
				continue
			}
		}
		filtered = append(filtered, existing)
	}
	if disabled && !already {
		filtered = append(filtered, needle)
	}
	c.DisabledPersonas = filtered
}

// GetSubagentType retrieves a subagent type configuration by ID or alias.
// Personas are catalog-fixed (loaded from pkg/personas/configs/*.json at
// startup) — there is no user-override merge path. Returns nil if the
// persona does not exist or has been disabled via Config.DisabledPersonas.
func (c *Config) GetSubagentType(id string) *SubagentType {
	if c == nil || c.SubagentTypes == nil {
		return nil
	}

	normalizedID := normalizePersonaID(id)
	if normalizedID == "" {
		return nil
	}

	// Resolve the request to a canonical map entry by ID, ID-field, or alias.
	var found *SubagentType
	var canonicalID string
	for personaID, subagentType := range c.SubagentTypes {
		st := subagentType
		switch {
		case normalizePersonaID(personaID) == normalizedID,
			normalizePersonaID(st.ID) == normalizedID:
			found = &st
			canonicalID = normalizePersonaID(st.ID)
		default:
			for _, alias := range st.Aliases {
				if normalizePersonaID(alias) == normalizedID {
					found = &st
					canonicalID = normalizePersonaID(st.ID)
					break
				}
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return nil
	}

	if c.IsPersonaDisabled(canonicalID) {
		return nil
	}

	// Deep copy slices so callers can't mutate the catalog-backed entry.
	result := *found
	result.AllowedTools = append([]string{}, found.AllowedTools...)
	result.Aliases = append([]string{}, found.Aliases...)
	result.Capabilities = append([]string{}, found.Capabilities...)
	result.CanSpawnNonDelegatable = append([]string{}, found.CanSpawnNonDelegatable...)
	if found.AutoApproveRules != nil {
		rules := *found.AutoApproveRules
		rules.LowRiskOps = append([]string{}, rules.LowRiskOps...)
		rules.MediumRiskOps = append([]string{}, rules.MediumRiskOps...)
		rules.HighRiskNever = append([]string{}, rules.HighRiskNever...)
		result.AutoApproveRules = &rules
	}
	return &result
}

func defaultSubagentTypes() map[string]SubagentType {
	definitions, err := personas.DefaultDefinitions()
	if err != nil {
		personaDefaultsWarningOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "WARNING: failed to load embedded persona definitions, using fallback defaults: %v\n", err)
		})
	}

	types := make(map[string]SubagentType, len(definitions))
	for id, definition := range definitions {
		autoApprove := convertAutoApproveRules(definition.AutoApproveRules)
		types[normalizePersonaID(id)] = SubagentType{
			ID:                     normalizePersonaID(definition.ID),
			Name:                   definition.Name,
			Description:            definition.Description,
			Provider:               definition.Provider,
			Model:                  definition.Model,
			SystemPrompt:           definition.SystemPrompt,
			SystemPromptText:       definition.SystemPromptText,
			SystemPromptAppend:     definition.SystemPromptAppend,
			AllowedTools:           append([]string{}, definition.AllowedTools...),
			Aliases:                append([]string{}, definition.Aliases...),
			Enabled:                definition.Enabled,
			LocalOnly:              definition.LocalOnly,
			Delegatable:            definition.Delegatable,
			AutoApproveRules:       autoApprove,
			Capabilities:           append([]string{}, definition.Capabilities...),
			CanSpawnNonDelegatable: append([]string{}, definition.CanSpawnNonDelegatable...),
		}
	}

	return types
}

// convertAutoApproveRules converts the persona catalog's AutoApproveRules to
// the configuration package's AutoApproveRules type, returning nil if the
// source is nil.
func convertAutoApproveRules(src *personas.AutoApproveRules) *AutoApproveRules {
	if src == nil {
		return nil
	}
	return &AutoApproveRules{
		LowRiskOps:    append([]string{}, src.LowRiskOps...),
		MediumRiskOps: append([]string{}, src.MediumRiskOps...),
		HighRiskNever: append([]string{}, src.HighRiskNever...),
	}
}

func normalizePersonaID(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

// GetSubagentTypeProvider returns the provider for a specific subagent type
// Falls back to the general subagent provider if not specified
func (c *Config) GetSubagentTypeProvider(id string) string {
	if st := c.GetSubagentType(id); st != nil && st.Provider != "" {
		return st.Provider
	}
	return c.GetSubagentProvider()
}

// GetSubagentTypeModel returns the model for a specific subagent type
// Falls back to the general subagent model if not specified
func (c *Config) GetSubagentTypeModel(id string) string {
	if st := c.GetSubagentType(id); st != nil && st.Model != "" {
		return st.Model
	}
	return c.GetSubagentModel()
}

// GetSubagentMaxParallel returns the maximum number of parallel subagents
// Defaults to 2 if not configured or set to 0
func (c *Config) GetSubagentMaxParallel() int {
	if c.SubagentMaxParallel > 0 {
		return c.SubagentMaxParallel
	}
	return 2 // Default
}

// GetSubagentParallelEnabled returns whether parallel subagent execution is enabled
// Defaults to true if not explicitly set (nil pointer)
func (c *Config) GetSubagentParallelEnabled() bool {
	if c.SubagentParallelEnabled == nil {
		return true // default when not configured
	}
	return *c.SubagentParallelEnabled
}

// GetSubagentMaxDepth returns the maximum subagent nesting depth.
// Defaults to 2 if not configured or set to 0.
func (c *Config) GetSubagentMaxDepth() int {
	if c.SubagentMaxDepth > 0 {
		return c.SubagentMaxDepth
	}
	return 2 // Default
}
