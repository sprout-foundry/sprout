package agent

import (
	"fmt"
	"sort"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
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

	// SP-050: the orchestrator persona's git-policy append rides on the
	// AllowOrchestratorGitWrite flag rather than living as a separate persona
	// (formerly repo_orchestrator). When the flag is on, append the embedded
	// policy markdown so the model knows about the commit tool, staging rules,
	// and which shell-side git ops are blocked.
	if personaID == "orchestrator" && config.AllowOrchestratorGitWrite {
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

// GetAvailableToolNames returns the effective tool names available to the active session.
func (a *Agent) GetAvailableToolNames() []string {
	tools := a.getOptimizedToolDefinitions(nil)
	if len(tools) == 0 {
		tools = api.GetToolDefinitions()
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

// isOrchestratorGitWriteAllowed returns true if the current agent is the
// orchestrator persona and the AllowOrchestratorGitWrite config is enabled.
// The legacy "repo_orchestrator" ID resolves to "orchestrator" via aliases.
func (a *Agent) isOrchestratorGitWriteAllowed() bool {
	persona := a.GetActivePersona()
	if persona != "orchestrator" {
		// Personas with auto-approve rules (e.g., executive_assistant) are treated
		// as having git write access when their rules include git write operations.
		if persona != "" && a.hasEAGitWriteApproval() {
			return true
		}
		return false
	}
	if a.configManager == nil {
		return false
	}
	config := a.configManager.GetConfig()
	if config == nil {
		return false
	}
	return config.AllowOrchestratorGitWrite
}

// hasEAGitWriteApproval checks if the active persona has auto-approve rules
// that explicitly include git write operations (git_commit, git_push, etc.)
// in its low or medium risk lists. This is used to grant git write access
// to the Executive Assistant persona.
func (a *Agent) hasEAGitWriteApproval() bool {
	cfg := a.GetConfig()
	if cfg == nil {
		return false
	}
	personaID := a.GetActivePersona()
	persona := cfg.GetSubagentType(personaID)
	if persona == nil || persona.AutoApproveRules == nil {
		return false
	}
	rules := persona.GetAutoApproveRules()
	gitWriteOps := []string{"git_commit", "git_push", "git_add"}
	for _, op := range gitWriteOps {
		for _, low := range rules.LowRiskOps {
			if low == op {
				return true
			}
		}
		for _, med := range rules.MediumRiskOps {
			if med == op {
				return true
			}
		}
	}
	return false
}

// hasEASpawnAuthority returns true if the active persona has EA-level spawn
// authority, allowing it to delegate to any persona regardless of the
// delegatable flag. This enables the three-level nesting chain:
// EA (depth 0) → orchestrator (depth 1) → coder/tester (depth 2).
//
// Authority is granted when:
// 1. The active persona is "executive_assistant", OR
// 2. The persona has auto-approve rules that include run_subagent
//    (indicating it operates with similar elevated authority)
func (a *Agent) hasEASpawnAuthority() bool {
	personaID := a.GetActivePersona()

	// Direct EA persona always has spawn authority
	if personaID == "executive_assistant" {
		return true
	}

	// Personas with auto-approve rules that include subagent spawning
	// are treated as having EA-level authority
	cfg := a.GetConfig()
	if cfg == nil {
		return false
	}
	persona := cfg.GetSubagentType(personaID)
	if persona == nil || persona.AutoApproveRules == nil {
		return false
	}
	rules := persona.GetAutoApproveRules()
	subagentOps := []string{"subagent_spawn"}
	for _, op := range subagentOps {
		for _, low := range rules.LowRiskOps {
			if low == op {
				return true
			}
		}
		for _, med := range rules.MediumRiskOps {
			if med == op {
				return true
			}
		}
	}
	return false
}
