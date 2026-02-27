package agent

import (
	"fmt"
	"sort"
	"strings"
)

// GetActivePersona returns the currently active persona ID.
func (a *Agent) GetActivePersona() string {
	return normalizeAgentPersonaID(a.activePersona)
}

// ClearActivePersona removes any active persona override and restores the base system prompt.
func (a *Agent) ClearActivePersona() {
	a.activePersona = ""
	if strings.TrimSpace(a.baseSystemPrompt) != "" {
		a.systemPrompt = a.baseSystemPrompt
	}
}

// ApplyPersona activates a configured persona and applies provider/model/system-prompt overrides.
func (a *Agent) ApplyPersona(personaID string) error {
	personaID = normalizeAgentPersonaID(personaID)
	if a.configManager == nil {
		return fmt.Errorf("configuration manager is not available")
	}

	config := a.configManager.GetConfig()
	if config == nil {
		return fmt.Errorf("configuration is not available")
	}

	persona := config.GetSubagentType(personaID)
	if persona == nil {
		return fmt.Errorf("persona not found or disabled: %s", personaID)
	}

	// Composition rules:
	// 1) Start from current provider/model.
	// 2) If persona provider is set, switch provider first (model falls back for that provider).
	// 3) If persona model is set, apply model on the effective provider.
	if strings.TrimSpace(persona.Provider) != "" {
		providerType, err := a.configManager.MapStringToClientType(strings.TrimSpace(persona.Provider))
		if err != nil {
			return fmt.Errorf("invalid persona provider %q: %w", persona.Provider, err)
		}
		if providerType != a.clientType {
			if err := a.SetProvider(providerType); err != nil {
				return fmt.Errorf("failed switching to persona provider %q: %w", persona.Provider, err)
			}
		}
	}

	if model := strings.TrimSpace(persona.Model); model != "" {
		if err := a.SetModel(model); err != nil {
			return fmt.Errorf("failed setting persona model %q: %w", model, err)
		}
	}

	// Persona prompt overrides only this session's active prompt.
	if promptText := strings.TrimSpace(persona.SystemPromptText); promptText != "" {
		a.SetSystemPrompt(promptText)
	} else if promptPath := strings.TrimSpace(persona.SystemPrompt); promptPath != "" {
		if err := a.SetSystemPromptFromFile(promptPath); err != nil {
			return fmt.Errorf("failed loading persona system prompt %q: %w", promptPath, err)
		}
	}

	a.activePersona = personaID
	return nil
}

func (a *Agent) getActivePersonaToolAllowlist() []string {
	activePersona := normalizeAgentPersonaID(a.activePersona)
	if activePersona == "" || a.configManager == nil {
		return nil
	}
	config := a.configManager.GetConfig()
	if config == nil {
		return nil
	}

	persona := config.GetSubagentType(activePersona)
	if persona == nil || len(persona.AllowedTools) == 0 {
		return nil
	}

	allowlist := make([]string, 0, len(persona.AllowedTools))
	for _, tool := range persona.AllowedTools {
		trimmed := strings.TrimSpace(tool)
		if trimmed == "" {
			continue
		}
		allowlist = append(allowlist, trimmed)
	}
	return allowlist
}

func normalizeAgentPersonaID(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

// GetAvailablePersonaIDs returns all configured persona IDs.
func (a *Agent) GetAvailablePersonaIDs() []string {
	if a.configManager == nil {
		return nil
	}
	config := a.configManager.GetConfig()
	if config == nil || config.SubagentTypes == nil {
		return nil
	}

	personaIDs := make([]string, 0, len(config.SubagentTypes))
	for id, persona := range config.SubagentTypes {
		if !persona.Enabled {
			continue
		}
		personaIDs = append(personaIDs, id)
	}
	sort.Strings(personaIDs)
	return personaIDs
}

// GetPersonaProviderModel returns effective provider/model for display.
func (a *Agent) GetPersonaProviderModel(personaID string) (string, string, error) {
	personaID = normalizeAgentPersonaID(personaID)
	if a.configManager == nil {
		return "", "", fmt.Errorf("configuration manager is not available")
	}
	config := a.configManager.GetConfig()
	if config == nil {
		return "", "", fmt.Errorf("configuration is not available")
	}
	persona := config.GetSubagentType(personaID)
	if persona == nil {
		return "", "", fmt.Errorf("persona not found or disabled: %s", personaID)
	}

	provider := strings.TrimSpace(string(a.clientType))
	if provider == "" {
		provider = strings.TrimSpace(a.GetProvider())
	}
	if strings.TrimSpace(persona.Provider) != "" {
		provider = strings.TrimSpace(persona.Provider)
	}

	model := a.GetModel()
	if strings.TrimSpace(persona.Provider) != "" && strings.TrimSpace(persona.Model) == "" {
		if providerType, err := a.configManager.MapStringToClientType(provider); err == nil {
			model = a.configManager.GetModelForProvider(providerType)
		}
	}
	if strings.TrimSpace(persona.Model) != "" {
		model = strings.TrimSpace(persona.Model)
	}

	return provider, model, nil
}
