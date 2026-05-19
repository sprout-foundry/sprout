package configuration

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// SubagentType defines a specialized subagent persona with its own configuration
type SubagentType struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Provider           string          `json:"provider"`
	Model              string          `json:"model"`
	SystemPrompt       string          `json:"system_prompt"`
	SystemPromptText   string          `json:"system_prompt_text,omitempty"`
	SystemPromptAppend string          `json:"system_prompt_append,omitempty"`
	AllowedTools       []string        `json:"allowed_tools,omitempty"`
	Aliases            []string        `json:"aliases,omitempty"`
	Enabled            bool            `json:"enabled"`
	LocalOnly          bool            `json:"local_only,omitempty"`
	AutoApproveRules   *AutoApproveRules `json:"auto_approve_rules,omitempty"`
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
		types[normalizePersonaID(id)] = SubagentType{
			ID:                 normalizePersonaID(definition.ID),
			Name:               definition.Name,
			Description:        definition.Description,
			Provider:           definition.Provider,
			Model:              definition.Model,
			SystemPrompt:       definition.SystemPrompt,
			SystemPromptText:   definition.SystemPromptText,
			SystemPromptAppend: definition.SystemPromptAppend,
			AllowedTools:       append([]string{}, definition.AllowedTools...),
			Aliases:            append([]string{}, definition.Aliases...),
			Enabled:            definition.Enabled,
			LocalOnly:          definition.LocalOnly,
		}
	}

	return types
}

func mergeMissingDefaultSubagentTypes(config *Config) {
	if config == nil {
		return
	}
	if config.SubagentTypes == nil {
		config.SubagentTypes = make(map[string]SubagentType)
	}

	for id, persona := range defaultSubagentTypes() {
		if _, exists := config.SubagentTypes[id]; !exists {
			config.SubagentTypes[id] = persona
		}
	}
}

func mergeLegacyStructuredToolsIntoPersonaAllowlists(config *Config) {
	if config == nil || config.SubagentTypes == nil {
		return
	}

	defaults := defaultSubagentTypes()
	for id, persona := range config.SubagentTypes {
		normalizedID := normalizePersonaID(id)
		if _, exists := defaults[normalizedID]; !exists {
			continue
		}
		if len(persona.AllowedTools) == 0 {
			continue
		}
		if !hasAnyTool(persona.AllowedTools, "write_file", "edit_file") {
			continue
		}

		changed := false
		if !hasTool(persona.AllowedTools, "write_structured_file") {
			persona.AllowedTools = append(persona.AllowedTools, "write_structured_file")
			changed = true
		}
		if !hasTool(persona.AllowedTools, "patch_structured_file") {
			persona.AllowedTools = append(persona.AllowedTools, "patch_structured_file")
			changed = true
		}

		if changed {
			config.SubagentTypes[id] = persona
		}
	}

	for id, persona := range config.SubagentTypes {
		normalizedID := normalizePersonaID(id)
		if normalizedID != "web_scraper" {
			continue
		}
		if len(persona.AllowedTools) == 0 {
			continue
		}
		if hasTool(persona.AllowedTools, "shell_command") {
			continue
		}
		persona.AllowedTools = append(persona.AllowedTools, "shell_command")
		config.SubagentTypes[id] = persona
	}
}

func hasAnyTool(tools []string, candidates ...string) bool {
	for _, candidate := range candidates {
		if hasTool(tools, candidate) {
			return true
		}
	}
	return false
}

func hasTool(tools []string, candidate string) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool) == candidate {
			return true
		}
	}
	return false
}

func normalizePersonaID(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

// GetSubagentType retrieves a subagent type configuration by ID
// Returns nil if the subagent type doesn't exist or is disabled
func (c *Config) GetSubagentType(id string) *SubagentType {
	if c.SubagentTypes == nil {
		return nil
	}

	normalizedID := normalizePersonaID(id)

	// Find user override if any and determine the primary ID
	var userOverride SubagentType
	userOverrideFound := false
	var primaryID string
	for personaID, subagentType := range c.SubagentTypes {
		normalizedPersonaID := normalizePersonaID(personaID)
		normalizedSubagentTypeID := normalizePersonaID(subagentType.ID)
		if normalizedPersonaID == normalizedID || normalizedSubagentTypeID == normalizedID {
			userOverride = subagentType
			userOverrideFound = true
			primaryID = normalizedSubagentTypeID
			break
		}
		for _, alias := range subagentType.Aliases {
			if normalizePersonaID(alias) == normalizedID {
				userOverride = subagentType
				userOverrideFound = true
				primaryID = normalizedSubagentTypeID
				break
			}
		}
		if userOverrideFound {
			break
		}
	}

	// Warn if multiple config entries could match the same normalized ID.
	if primaryID != "" && primaryID != normalizedID {
		for k := range c.SubagentTypes {
			if normalizePersonaID(k) == normalizedID && normalizePersonaID(k) != primaryID {
				log.Printf("[config] WARNING: multiple subagent config entries match %q — behavior is non-deterministic due to map iteration order", normalizedID)
				break
			}
		}
	}

	// Get the default persona definition using the primary ID
	defaultPersonas := defaultSubagentTypes()
	var defaultPersona SubagentType
	defaultExists := false
	if primaryID != "" {
		defaultPersona, defaultExists = defaultPersonas[primaryID]
	} else {
		defaultPersona, defaultExists = defaultPersonas[normalizedID]
	}

	// If no default exists and no user override, persona doesn't exist
	if !defaultExists && !userOverrideFound {
		return nil
	}

	// Custom persona: only exists in user config, not in defaults
	if !defaultExists && userOverrideFound {
		if !userOverride.Enabled {
			return nil
		}
		result := userOverride
		result.AllowedTools = append([]string{}, userOverride.AllowedTools...)
		result.Aliases = append([]string{}, userOverride.Aliases...)
		if userOverride.AutoApproveRules != nil {
			rules := *userOverride.AutoApproveRules
			rules.LowRiskOps = append([]string{}, rules.LowRiskOps...)
			rules.MediumRiskOps = append([]string{}, rules.MediumRiskOps...)
			rules.HighRiskNever = append([]string{}, rules.HighRiskNever...)
			result.AutoApproveRules = &rules
		}
		return &result
	}

	// Default persona with user override
	if defaultExists {
		if userOverrideFound && !userOverride.Enabled {
			return nil
		}

		result := SubagentType{
			ID:                 defaultPersona.ID,
			Name:               defaultPersona.Name,
			Description:        defaultPersona.Description,
			Provider:           defaultPersona.Provider,
			Model:              defaultPersona.Model,
			SystemPrompt:       defaultPersona.SystemPrompt,
			SystemPromptText:   defaultPersona.SystemPromptText,
			SystemPromptAppend: defaultPersona.SystemPromptAppend,
			AllowedTools:       make([]string, len(defaultPersona.AllowedTools)),
			Aliases:            make([]string, len(defaultPersona.Aliases)),
			Enabled:            defaultPersona.Enabled,
			LocalOnly:          defaultPersona.LocalOnly,
		}
		copy(result.AllowedTools, defaultPersona.AllowedTools)
		copy(result.Aliases, defaultPersona.Aliases)

		// Deep copy auto-approve rules
		if defaultPersona.AutoApproveRules != nil {
			rules := *defaultPersona.AutoApproveRules
			rules.LowRiskOps = append([]string{}, rules.LowRiskOps...)
			rules.MediumRiskOps = append([]string{}, rules.MediumRiskOps...)
			rules.HighRiskNever = append([]string{}, rules.HighRiskNever...)
			result.AutoApproveRules = &rules
		}

		// If user has override, overlay only the user-overridable fields
		if userOverrideFound {
			if userOverride.Provider != "" {
				result.Provider = userOverride.Provider
			}
			if userOverride.Model != "" {
				result.Model = userOverride.Model
			}
			if userOverride.SystemPromptAppend != "" {
				result.SystemPromptAppend = userOverride.SystemPromptAppend
			}
			if userOverride.LocalOnly {
				result.LocalOnly = true
			}
			if userOverride.AutoApproveRules != nil {
				rules := *userOverride.AutoApproveRules
				rules.LowRiskOps = append([]string{}, rules.LowRiskOps...)
				rules.MediumRiskOps = append([]string{}, rules.MediumRiskOps...)
				rules.HighRiskNever = append([]string{}, rules.HighRiskNever...)
				result.AutoApproveRules = &rules
			}
		}

		if result.Enabled {
			return &result
		}
		return nil
	}

	return nil
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
