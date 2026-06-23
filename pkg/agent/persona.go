package agent

import (
	"fmt"
	"sort"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// GetActivePersona returns the currently active persona ID.
func (a *Agent) GetActivePersona() string {
	return normalizeAgentPersonaID(a.state.GetActivePersona())
}

// ClearActivePersona removes any active persona override and restores the base system prompt.
func (a *Agent) ClearActivePersona() {
	a.state.SetActivePersona("")
	if strings.TrimSpace(a.baseSystemPrompt) != "" {
		a.systemPrompt = a.baseSystemPrompt
	}
}

// ApplyPersona activates a configured persona and applies provider/model/system-prompt overrides.
func (a *Agent) ApplyPersona(personaID string) error {
	personaID = normalizeAgentPersonaID(personaID)
	if a.configManager == nil {
		return fmt.Errorf("agent configuration manager is not available for persona %q", personaID)
	}

	config := a.configManager.GetConfig()
	if config == nil {
		return fmt.Errorf("agent configuration is not available for persona %q", personaID)
	}

	persona := config.GetSubagentType(personaID)
	if persona == nil {
		available := a.GetAvailablePersonaIDs()
		if len(available) == 0 {
			return fmt.Errorf("persona not found or disabled: %s (no enabled personas configured)", personaID)
		}
		return fmt.Errorf("persona not found or disabled: %s (available personas: %s)", personaID, strings.Join(available, ", "))
	}
	// Canonicalize the persona ID: an alias (e.g. legacy "repo_orchestrator")
	// resolves to its primary ID (e.g. "orchestrator") via GetSubagentType, and
	// we store the canonical form so downstream checks key off one name.
	if canonical := normalizeAgentPersonaID(persona.ID); canonical != "" {
		personaID = canonical
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
		if providerType != a.getClientType() {
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
	// system_prompt_text: completely replaces the current prompt.
	// system_prompt_append: appends to the current prompt (useful for adding
	// persona-specific rules on top of the base orchestrator prompt).
	if promptText := strings.TrimSpace(persona.SystemPromptText); promptText != "" {
		a.SetSystemPrompt(promptText)
	} else if promptPath := strings.TrimSpace(persona.SystemPrompt); promptPath != "" {
		if err := a.SetSystemPromptFromFile(promptPath); err != nil {
			return fmt.Errorf("failed loading persona system prompt %q: %w", promptPath, err)
		}
	}

	// Append supplement after the base/file/text prompt is set.
	if appendText := strings.TrimSpace(persona.SystemPromptAppend); appendText != "" {
		current := a.GetSystemPrompt()
		if strings.TrimSpace(current) != "" {
			a.SetSystemPrompt(current + "\n\n---\n\n" + appendText)
		} else {
			a.SetSystemPrompt(appendText)
		}
	}

	// SP-050: the orchestrator persona always gets the git-policy append.
	// The policy text documents the commit tool preference, staging rules,
	// and which shell-side git ops are blocked.
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

	// When the primary agent (depth 0) sets its persona, record it as the root persona.
	// Subagents inherit this through rootPersonaID propagation.
	if a.subagentDepth == 0 {
		a.rootPersonaID = personaID
	}

	// SP-051: keep the depth/persona event-metadata in sync with the active
	// persona so every event the agent publishes is tagged. Subagents get
	// theirs at creation in subagent_runner.createSubagent; this covers the
	// primary agent and any later persona switches mid-session.
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

// GetAvailablePersonaIDs returns all configured persona IDs,
// filtering out LocalOnly personas when running in cloud mode.
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

// GetPersonaProviderModel returns effective provider/model for display.
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
		return "", "", fmt.Errorf("persona not found or disabled: %s", personaID)
	}

	provider := strings.TrimSpace(string(a.getClientType()))
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

// GetAvailableToolNames returns the effective tool names available to the active session.
func (a *Agent) GetAvailableToolNames() []string {
	tools := a.getOptimizedToolDefinitions(nil)
	if len(tools) == 0 {
		tools = BuildToolDefinitions()
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

// isGitWriteAllowed returns true if the active persona is permitted to perform
// git write operations (commit, stage, push) via shell_command or the commit
// tool. The gate is the persona's CapabilityGitWrite capability — personas that
// declare it (orchestrator, coordinator) are allowed; all others are not.
//
// The ChangeTracker provides the recovery safety net for git operations, so no
// additional config toggle is needed.
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

// canSpawnNonDelegatable reports whether the active persona is permitted to
// spawn the given target persona ID, even if the target carries
// Delegatable=false. The check reads the active persona's
// CanSpawnNonDelegatable list — declarative replacement for the previous
// hasEASpawnAuthority special case. The coordinator declares ["orchestrator"]
// so the canonical coordinator→orchestrator→specialist chain works without
// special-case Go code.
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

