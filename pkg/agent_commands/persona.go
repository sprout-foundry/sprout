package commands

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// PersonaCommand implements the /persona slash command.
//
// Personas are catalog-fixed: the set of personas, their tool allowlists,
// system prompts, providers, and models are defined in
// pkg/personas/configs/*.json and are NOT user-customizable at runtime.
// The only mutable knob is enable/disable, which is recorded by ID in
// Config.DisabledPersonas so the catalog itself never gets mutated.
//
// Workflow-level provider/model overrides (sprout automate) and per-spawn
// overrides (`run_subagent` arguments) remain available — those are
// transient, programmatic, and tracked separately.
type PersonaCommand struct{}

func (p *PersonaCommand) Name() string        { return "persona" }
func (p *PersonaCommand) Description() string { return "List, activate, and enable/disable personas" }

// Usage returns the detailed help text shown by `/help persona`.
func (p *PersonaCommand) Usage() string {
	return strings.Join([]string{
		"/persona [list]              List all personas with active/disabled status.",
		"/persona <name>              Activate <name> for the current session.",
		"/persona <name> show         Show persona details (provider, model, tools).",
		"/persona <name> enable       Re-enable a disabled persona.",
		"/persona <name> disable      Disable a persona (it can't be spawned).",
		"/persona clear               Clear the active persona.",
		"",
		"Personas are catalog-fixed; only enable/disable is mutable at runtime.",
		"Aliases: /subagent-persona",
	}, "\n")
}

// SubagentPersonaCommand is a backwards-compatible alias for /persona.
type SubagentPersonaCommand struct{}

func (s *SubagentPersonaCommand) Name() string        { return "subagent-persona" }
func (s *SubagentPersonaCommand) Description() string { return "Alias for /persona" }

// Usage returns the detailed help text shown by `/help subagent-persona`.
func (s *SubagentPersonaCommand) Usage() string { return (&PersonaCommand{}).Usage() }

func (s *SubagentPersonaCommand) Execute(args []string, chatAgent *agent.Agent) error {
	return (&PersonaCommand{}).Execute(args, chatAgent)
}

// SubagentPersonasCommand is a backwards-compatible alias for /persona list.
type SubagentPersonasCommand struct{}

func (s *SubagentPersonasCommand) Name() string        { return "subagent-personas" }
func (s *SubagentPersonasCommand) Description() string { return "Alias for /persona list" }

// Usage returns the detailed help text shown by `/help subagent-personas`.
func (s *SubagentPersonasCommand) Usage() string { return (&PersonaCommand{}).Usage() }

func (s *SubagentPersonasCommand) Execute(args []string, chatAgent *agent.Agent) error {
	return (&PersonaCommand{}).Execute(nil, chatAgent)
}

func (p *PersonaCommand) Execute(args []string, chatAgent *agent.Agent) error {
	configManager := chatAgent.GetConfigManager()
	config := configManager.GetConfig()
	if config == nil {
		return errors.New("configuration not available")
	}

	if len(args) == 0 || strings.EqualFold(args[0], "list") {
		return p.listPersonas(config, chatAgent)
	}

	if strings.EqualFold(args[0], "none") || strings.EqualFold(args[0], "clear") {
		chatAgent.ClearActivePersona()
		console.GlyphSuccess.Print("Cleared active persona; restored base system prompt")
		return nil
	}

	personaID, persona, ok := resolvePersona(config, args[0])
	if !ok {
		return fmt.Errorf("persona not found: %s", args[0])
	}

	if len(args) == 1 || strings.EqualFold(args[1], "apply") {
		if err := chatAgent.ApplyPersona(personaID); err != nil {
			return fmt.Errorf("persona apply: %w", err)
		}
		provider, model, _ := chatAgent.GetPersonaProviderModel(personaID)
		console.GlyphSuccess.Printf("Active persona: %s (%s)", persona.Name, personaID)
		fmt.Printf("   Provider: %s\n", provider)
		fmt.Printf("   Model: %s\n", model)
		return nil
	}

	action := strings.ToLower(args[1])
	switch action {
	case "show":
		return p.showPersona(personaID, *persona, chatAgent)
	case "enable":
		return setPersonaDisabled(personaID, false, configManager)
	case "disable":
		return setPersonaDisabled(personaID, true, configManager)
	default:
		return fmt.Errorf("unknown action: %s (valid: apply, show, enable, disable, clear)", action)
	}
}

func (p *PersonaCommand) listPersonas(config *configuration.Config, chatAgent *agent.Agent) error {
	fmt.Println()
	console.GlyphInfo.Print("Personas")
	active := chatAgent.GetActivePersona()
	if active == "" {
		active = "<none>"
	}
	fmt.Printf("Active: %s\n", active)

	ids := make([]string, 0, len(config.SubagentTypes))
	for id := range config.SubagentTypes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	isLocal := chatAgent.IsLocalMode()
	for _, id := range ids {
		persona := config.SubagentTypes[id]
		status := "enabled"
		if config.IsPersonaDisabled(id) {
			status = "disabled"
		}
		// Surface LocalOnly availability so the user understands why a persona
		// they see here might not be spawnable in cloud mode.
		modifier := ""
		if persona.LocalOnly {
			if isLocal {
				modifier = " (local only)"
			} else {
				modifier = " (local only — unavailable in cloud)"
			}
		}
		fmt.Printf("- %s (%s): %s%s\n", id, persona.Name, status, modifier)
	}

	fmt.Println("\nUsage:")
	fmt.Println("  /persona <name>                 - Activate persona now")
	fmt.Println("  /persona <name> show            - Show persona details")
	fmt.Println("  /persona <name> enable|disable  - Toggle availability")
	fmt.Println("  /persona clear                  - Clear active persona")
	return nil
}

func (p *PersonaCommand) showPersona(personaID string, persona configuration.SubagentType, chatAgent *agent.Agent) error {
	provider, model, _ := chatAgent.GetPersonaProviderModel(personaID)
	cfg := chatAgent.GetConfigManager().GetConfig()
	enabled := cfg == nil || !cfg.IsPersonaDisabled(personaID)

	fmt.Println()
	console.GlyphInfo.Printf("%s (%s)", persona.Name, personaID)
	fmt.Printf("Description: %s\n", persona.Description)
	fmt.Printf("Enabled: %t\n", enabled)
	fmt.Printf("Provider: %s\n", provider)
	fmt.Printf("Model: %s\n", model)
	if len(persona.AllowedTools) > 0 {
		fmt.Printf("Allowed tools: %s\n", strings.Join(persona.AllowedTools, ", "))
	}
	if strings.TrimSpace(persona.SystemPrompt) != "" {
		fmt.Printf("System prompt: %s\n", persona.SystemPrompt)
	}
	return nil
}

// setPersonaDisabled writes the (canonical) persona ID into / out of
// Config.DisabledPersonas. The catalog itself is never mutated.
func setPersonaDisabled(personaID string, disabled bool, cm *configuration.Manager) error {
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.SetPersonaDisabled(personaID, disabled)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	verb := "enabled"
	if disabled {
		verb = "disabled"
	}
	console.GlyphSuccess.Printf("Persona %s %s", personaID, verb)
	return nil
}

func resolvePersona(config *configuration.Config, raw string) (string, *configuration.SubagentType, bool) {
	if config == nil || config.SubagentTypes == nil {
		return "", nil, false
	}
	needle := normalizePersonaKey(raw)
	for id, persona := range config.SubagentTypes {
		if normalizePersonaKey(id) == needle || normalizePersonaKey(persona.Name) == needle {
			p := persona
			return id, &p, true
		}
		for _, alias := range persona.Aliases {
			if normalizePersonaKey(alias) == needle {
				p := persona
				return id, &p, true
			}
		}
	}
	return "", nil, false
}

func normalizePersonaKey(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

// Complete returns completions for the /persona command.
func (p *PersonaCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	baseCommands := []string{"clear", "list"}

	if len(args) == 0 {
		// Combine base commands with available persona names from config.
		if chatAgent == nil {
			return baseCommands
		}
		mgr := chatAgent.GetConfigManager()
		if mgr == nil {
			return baseCommands
		}
		cfg := mgr.GetConfig()
		if cfg == nil || cfg.SubagentTypes == nil || len(cfg.SubagentTypes) == 0 {
			return baseCommands
		}
		all := make([]string, 0, len(baseCommands)+len(cfg.SubagentTypes))
		all = append(all, baseCommands...)
		for id := range cfg.SubagentTypes {
			all = append(all, id)
		}
		sort.Strings(all)
		return all
	}

	// Build candidate list: base commands + persona names.
	candidates := make([]string, 0, len(baseCommands))
	candidates = append(candidates, baseCommands...)
	if chatAgent != nil {
		mgr := chatAgent.GetConfigManager()
		if mgr != nil {
			cfg := mgr.GetConfig()
			if cfg != nil && cfg.SubagentTypes != nil {
				for id := range cfg.SubagentTypes {
					candidates = append(candidates, id)
				}
			}
		}
	}

	prefix := args[len(args)-1]
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(prefix)) {
			matches = append(matches, candidate)
		}
	}
	sort.Strings(matches)
	return matches
}
