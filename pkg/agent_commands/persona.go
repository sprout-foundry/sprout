package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/personas"
)

// PersonaCommand implements the /persona slash command.
type PersonaCommand struct{}

// Name returns the command name.
func (p *PersonaCommand) Name() string {
	return "persona"
}

// Description returns the command description.
func (p *PersonaCommand) Description() string {
	return "Manage and apply focused personas (provider/model/tools/system prompt)"
}

// Execute runs the persona command.
func (p *PersonaCommand) Execute(args []string, chatAgent *agent.Agent) error {
	configManager := chatAgent.GetConfigManager()
	config := configManager.GetConfig()
	if config == nil {
		return fmt.Errorf("configuration not available")
	}
	if config.SubagentTypes == nil {
		config.SubagentTypes = make(map[string]configuration.SubagentType)
	}

	if len(args) == 0 || args[0] == "list" {
		return p.listPersonas(config, chatAgent)
	}

	if strings.EqualFold(args[0], "none") || strings.EqualFold(args[0], "clear") {
		chatAgent.ClearActivePersona()
		fmt.Println("✅ Cleared active persona; restored base system prompt")
		return nil
	}

	if strings.EqualFold(args[0], "create") {
		if len(args) < 2 {
			return fmt.Errorf("usage: /persona create <persona-id>")
		}
		return p.createPersona(args[1], config, configManager)
	}

	personaID, persona, ok := resolvePersona(config, args[0])
	if !ok {
		return fmt.Errorf("persona not found: %s", args[0])
	}

	if len(args) == 1 || strings.EqualFold(args[1], "apply") {
		if err := chatAgent.ApplyPersona(personaID); err != nil {
			return err
		}
		provider, model, _ := chatAgent.GetPersonaProviderModel(personaID)
		fmt.Printf("✅ Active persona: %s (%s)\n", persona.Name, personaID)
		fmt.Printf("   Provider: %s\n", provider)
		fmt.Printf("   Model: %s\n", model)
		return nil
	}

	action := strings.ToLower(args[1])
	switch action {
	case "show":
		return p.showPersona(personaID, *persona, chatAgent)
	case "enable":
		persona.Enabled = true
		config.SubagentTypes[personaID] = *persona
		if err := configManager.SaveConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Printf("✅ Enabled persona %s\n", personaID)
		return nil
	case "disable":
		persona.Enabled = false
		config.SubagentTypes[personaID] = *persona
		if err := configManager.SaveConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Printf("✅ Disabled persona %s\n", personaID)
		return nil
	case "provider":
		if len(args) < 3 {
			return fmt.Errorf("usage: /persona %s provider <provider|default>", personaID)
		}
		if strings.EqualFold(args[2], "default") {
			persona.Provider = ""
		} else {
			providerType, err := configManager.MapStringToClientType(args[2])
			if err != nil {
				return fmt.Errorf("invalid provider: %s", args[2])
			}
			persona.Provider = string(providerType)
		}
	case "model":
		if len(args) < 3 {
			return fmt.Errorf("usage: /persona %s model <model|default>", personaID)
		}
		if strings.EqualFold(args[2], "default") {
			persona.Model = ""
		} else {
			persona.Model = strings.TrimSpace(args[2])
		}
	case "tools":
		if len(args) < 3 {
			return fmt.Errorf("usage: /persona %s tools <csv-tools|default>", personaID)
		}
		toolsArg := strings.Join(args[2:], " ")
		if strings.EqualFold(strings.TrimSpace(toolsArg), "default") {
			persona.AllowedTools = nil
		} else {
			persona.AllowedTools = parseCommaList(toolsArg)
			if unknown := configuration.UnknownPersonaTools(persona.AllowedTools); len(unknown) > 0 {
				fmt.Fprintf(os.Stderr, "⚠️  Unknown tools in allowlist: %s\n", strings.Join(unknown, ", "))
			}
		}
	case "prompt":
		if len(args) < 3 {
			return fmt.Errorf("usage: /persona %s prompt <file-path|default>", personaID)
		}
		if strings.EqualFold(args[2], "default") {
			persona.SystemPrompt = ""
		} else {
			promptPath := strings.TrimSpace(args[2])
			if _, err := os.Stat(resolveRepoRelativePath(promptPath)); err != nil {
				return fmt.Errorf("prompt file not accessible: %s", promptPath)
			}
			persona.SystemPrompt = promptPath
		}
	case "prompt-str":
		if len(args) < 3 {
			return fmt.Errorf("usage: /persona %s prompt-str <text|default>", personaID)
		}
		text := strings.TrimSpace(strings.Join(args[2:], " "))
		if strings.EqualFold(text, "default") {
			persona.SystemPromptText = ""
		} else {
			persona.SystemPromptText = text
		}
	default:
		return fmt.Errorf("unknown action: %s", action)
	}

	config.SubagentTypes[personaID] = *persona
	if err := configManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Updated persona %s\n", personaID)
	fmt.Println("💡 Activate it with: /persona " + personaID)
	return nil
}

func (p *PersonaCommand) listPersonas(config *configuration.Config, chatAgent *agent.Agent) error {
	fmt.Println("\n🎭 Personas")
	fmt.Println("===========")
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

	for _, id := range ids {
		persona := config.SubagentTypes[id]
		status := "enabled"
		if !persona.Enabled {
			status = "disabled"
		}
		fmt.Printf("- %s (%s): %s\n", id, persona.Name, status)
	}

	fmt.Println("\nUsage:")
	fmt.Println("  /persona <name>                        - Activate persona now")
	fmt.Println("  /persona <name> provider <p|default>   - Set provider override")
	fmt.Println("  /persona <name> model <m|default>      - Set model override")
	fmt.Println("  /persona <name> tools <csv|default>    - Set allowed tools")
	fmt.Println("  /persona <name> prompt <path|default>  - Set system prompt file")
	fmt.Println("  /persona <name> prompt-str <text>      - Set inline system prompt")
	fmt.Println("  /persona create <name>                 - Create custom persona")
	fmt.Println("  /persona clear                         - Clear active persona")
	return nil
}

func (p *PersonaCommand) showPersona(personaID string, persona configuration.SubagentType, chatAgent *agent.Agent) error {
	provider, model, _ := chatAgent.GetPersonaProviderModel(personaID)
	fmt.Printf("\n🎭 %s (%s)\n", persona.Name, personaID)
	fmt.Printf("Description: %s\n", persona.Description)
	fmt.Printf("Enabled: %t\n", persona.Enabled)
	fmt.Printf("Provider: %s\n", provider)
	fmt.Printf("Model: %s\n", model)
	if len(persona.AllowedTools) > 0 {
		fmt.Printf("Allowed tools: %s\n", strings.Join(persona.AllowedTools, ", "))
	} else {
		fmt.Println("Allowed tools: <default>")
	}
	if strings.TrimSpace(persona.SystemPromptText) != "" {
		fmt.Printf("System prompt: inline (%d chars)\n", len(persona.SystemPromptText))
	} else if strings.TrimSpace(persona.SystemPrompt) != "" {
		fmt.Printf("System prompt: %s\n", persona.SystemPrompt)
	} else {
		fmt.Println("System prompt: <default>")
	}
	return nil
}

func (p *PersonaCommand) createPersona(personaID string, config *configuration.Config, configManager *configuration.Manager) error {
	personaID = normalizePersonaKey(personaID)
	if personaID == "" {
		return fmt.Errorf("persona id cannot be empty")
	}
	if _, exists := config.SubagentTypes[personaID]; exists {
		return fmt.Errorf("persona already exists: %s", personaID)
	}

	config.SubagentTypes[personaID] = buildCustomPersonaTemplate(personaID)

	if err := configManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Created persona %s\n", personaID)
	fmt.Printf("💡 Configure it with: /persona %s show\n", personaID)
	return nil
}

func buildCustomPersonaTemplate(personaID string) configuration.SubagentType {
	title := personaTitle(personaID)
	defaultPrompt := filepath.Join("pkg", "agent", "prompts", "subagent_prompts", "general.md")
	defaultTools := []string{"read_file", "search_files", "TodoWrite", "TodoRead"}
	if definitions, err := personas.DefaultDefinitions(); err == nil {
		if general, exists := definitions["general"]; exists {
			if strings.TrimSpace(general.SystemPrompt) != "" {
				defaultPrompt = strings.TrimSpace(general.SystemPrompt)
			}
			if len(general.AllowedTools) > 0 {
				defaultTools = append([]string{}, general.AllowedTools...)
			}
		}
	}
	return configuration.SubagentType{
		ID:           personaID,
		Name:         title,
		Description:  "Custom persona",
		SystemPrompt: defaultPrompt,
		AllowedTools: defaultTools,
		Enabled:      true,
	}
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

	// Backward compatibility: map old persona names to new ones
	legacyMappings := map[string]string{
		"qa_engineer":   "tester",
		"web_researcher": "web_scraper",
	}
	if mappedID, exists := legacyMappings[needle]; exists {
		// Try to find the new persona ID (case-insensitive)
		for id, persona := range config.SubagentTypes {
			if normalizePersonaKey(id) == normalizePersonaKey(mappedID) || normalizePersonaKey(persona.Name) == normalizePersonaKey(mappedID) {
				p := persona
				return id, &p, true
			}
		}
	}

	return "", nil, false
}

func parseCommaList(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func normalizePersonaKey(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func personaTitle(personaID string) string {
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(personaID, "_", " "), "-", " "))
	if len(words) == 0 {
		return ""
	}

	for i, word := range words {
		runes := []rune(word)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}

	return strings.Join(words, " ")
}

func resolveRepoRelativePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}

	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			candidate := filepath.Join(dir, path)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		dir = parent
	}
}
