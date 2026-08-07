package agent

import (
	"fmt"
	"sort"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

func (a *Agent) GetActivePersona() string {
	return normalizeAgentPersonaID(a.state.GetActivePersona())
}

func (a *Agent) ClearActivePersona() {
	a.state.SetActivePersona("")
	if strings.TrimSpace(a.baseSystemPrompt) != "" {
		a.systemPrompt = a.baseSystemPrompt
	}
}

func (a *Agent) ApplyPersona(personaID string) error {
	personaID = normalizeAgentPersonaID(personaID)
	if a.configManager == nil {
		return agenterrors.NewConfig(fmt.Sprintf("agent configuration manager is not available for persona %q", personaID), nil)
	}

	config := a.configManager.GetConfig()
	if config == nil {
		return agenterrors.NewConfig(fmt.Sprintf("agent configuration is not available for persona %q", personaID), nil)
	}

	persona := config.GetSubagentType(personaID)
	if persona == nil {
		available := a.GetAvailablePersonaIDs()
		if len(available) == 0 {
			return agenterrors.NewNotFound(fmt.Sprintf("persona %s (no enabled personas configured)", personaID))
		}
		return agenterrors.NewNotFound(fmt.Sprintf("persona %s (available personas: %s)", personaID, strings.Join(available, ", ")))
	}
	if canonical := normalizeAgentPersonaID(persona.ID); canonical != "" {
		personaID = canonical
	}

	if personaID == personas.IDComputerUser {
		if err := a.checkComputerUseActivation(); err != nil {
			return err
		}
	}

	if strings.TrimSpace(persona.Provider) != "" {
		providerType, err := a.configManager.MapStringToClientType(strings.TrimSpace(persona.Provider))
		if err != nil {
			return agenterrors.NewConfig(fmt.Sprintf("invalid persona provider %q", persona.Provider), err)
		}
		if providerType != a.getClientType() {
			if err := a.SetProvider(providerType); err != nil {
				return agenterrors.NewConfig(fmt.Sprintf("failed switching to persona provider %q", persona.Provider), err)
			}
		}
	}

	if model := strings.TrimSpace(persona.Model); model != "" {
		if err := a.SetModel(model); err != nil {
			return agenterrors.NewConfig(fmt.Sprintf("failed setting persona model %q", model), err)
		}
	}

	if promptText := strings.TrimSpace(persona.SystemPromptText); promptText != "" {
		a.SetSystemPrompt(promptText)
	} else if promptPath := strings.TrimSpace(persona.SystemPrompt); promptPath != "" {
		if err := a.SetSystemPromptFromFile(promptPath); err != nil {
			return agenterrors.NewConfig(fmt.Sprintf("failed loading persona system prompt %q", promptPath), err)
		}
	}

	if appendText := strings.TrimSpace(persona.SystemPromptAppend); appendText != "" {
		current := a.GetSystemPrompt()
		if strings.TrimSpace(current) != "" {
			a.SetSystemPrompt(current + "\n\n---\n\n" + appendText)
		} else {
			a.SetSystemPrompt(appendText)
		}
	}

	if personaID == personas.IDOrchestrator {
		if policy := strings.TrimSpace(orchestratorGitPolicyAppend); policy != "" {
			current := a.GetSystemPrompt()
			if strings.TrimSpace(current) != "" {
				a.SetSystemPrompt(current + "\n\n---\n\n" + policy)
			} else {
				a.SetSystemPrompt(policy)
			}
		}
	}

	a.state.SetActivePersona(personaID)

	if personaID == personas.IDComputerUser {
		SetActiveComputerUseAgent(a)

		a.PublishAgentMessage("warning", "⚠  COMPUTER USE ACTIVE — The agent can now control your mouse, keyboard, and screen. Watch the screen. Stop the agent (Ctrl+C) if it does something unexpected. Per-session opt-in, panic key, and destructive-app blocking are NOT yet implemented.", nil)
	} else {
		if prev := a.state.GetActivePersona(); prev == personas.IDComputerUser || personaID != personas.IDComputerUser {
			SetActiveComputerUseAgent(nil)
		}
	}

	if a.subagentDepth == 0 {
		a.rootPersonaID = personaID
	}

	a.MergeEventMetadata(map[string]interface{}{
		"subagent_depth": a.subagentDepth,
		"active_persona": personaID,
	})

	return nil
}

func (a *Agent) getActivePersonaToolAllowlist() []string {
	activePersona := normalizeAgentPersonaID(a.state.GetActivePersona())
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

func (a *Agent) GetAvailablePersonaIDs() []string {
	if a.configManager == nil {
		return nil
	}
	config := a.configManager.GetConfig()
	if config == nil || config.SubagentTypes == nil {
		return nil
	}

	isLocal := a.IsLocalMode()

	personaIDs := make([]string, 0, len(config.SubagentTypes))
	for id, persona := range config.SubagentTypes {
		if !persona.Enabled {
			continue
		}
		// Filter out user-disabled personas (DisabledPersonas takes precedence
		// over the catalog's Enabled flag).
		if config.IsPersonaDisabled(id) {
			continue
		}
		// Filter out LocalOnly personas in cloud mode
		if persona.LocalOnly && !isLocal {
			continue
		}
		personaIDs = append(personaIDs, id)
	}
	sort.Strings(personaIDs)
	return personaIDs
}

func (a *Agent) GetPersonaProviderModel(personaID string) (string, string, error) {
	personaID = normalizeAgentPersonaID(personaID)
	if a.configManager == nil {
		return "", "", agenterrors.NewPermanentError("configuration manager is not available", nil)
	}
	config := a.configManager.GetConfig()
	if config == nil {
		return "", "", agenterrors.NewPermanentError("configuration is not available", nil)
	}
	persona := config.GetSubagentType(personaID)
	if persona == nil {
		return "", "", agenterrors.NewNotFound(fmt.Sprintf("persona %s", personaID))
	}

	// Resolve provider: persona → config.SubagentProvider → parent runtime provider.
	provider := strings.TrimSpace(persona.Provider)
	if provider == "" {
		provider = strings.TrimSpace(config.SubagentProvider)
	}
	if provider == "" {
		provider = a.parentRuntimeProvider()
	}

	// Resolve model: persona → config.SubagentModel → provider default → current model.
	model := strings.TrimSpace(persona.Model)
	if model == "" {
		model = strings.TrimSpace(config.SubagentModel)
	}
	if model == "" {
		if providerType, err := a.configManager.MapStringToClientType(provider); err == nil {
			model = a.configManager.GetModelForProvider(providerType)
		}
	}
	if model == "" {
		model = a.GetModel()
	}

	return provider, model, nil
}

// parentRuntimeProvider returns the parent agent's effective provider key.
func (a *Agent) parentRuntimeProvider() string {
	if p := strings.TrimSpace(string(a.getClientType())); p != "" {
		return p
	}
	return strings.TrimSpace(a.GetProvider())
}

func (a *Agent) GetAvailableToolNames() []string {
	tools := a.getOptimizedToolDefinitions(nil)
	if len(tools) == 0 {
		tools = BuildToolDefinitionsForAgent(a)
	}

	names := make([]string, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (a *Agent) isGitWriteAllowed() bool {
	personaID := a.GetActivePersona()
	if personaID == "" {
		return false
	}
	cfg := a.GetConfig()
	if cfg == nil {
		return false
	}
	persona := cfg.GetSubagentType(personaID)
	if persona == nil {
		return false
	}
	return persona.HasCapability(personas.CapabilityGitWrite)
}

func (a *Agent) canSpawnNonDelegatable(target string) bool {
	cfg := a.GetConfig()
	if cfg == nil {
		return false
	}
	spawner := cfg.GetSubagentType(a.GetActivePersona())
	if spawner == nil {
		return false
	}
	normalizedTarget := normalizeAgentPersonaID(target)
	for _, allowed := range spawner.CanSpawnNonDelegatable {
		if normalizeAgentPersonaID(allowed) == normalizedTarget {
			return true
		}
	}
	return false
}
