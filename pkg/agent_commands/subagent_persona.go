package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// SubagentPersonasCommand implements the /subagent-personas command (list all personas)
type SubagentPersonasCommand struct{}

// Name returns the command name
func (s *SubagentPersonasCommand) Name() string {
	return "subagent-personas"
}

// Description returns the command description
func (s *SubagentPersonasCommand) Description() string {
	return "List all available subagent personas"
}

// Execute runs the subagent personas command
func (s *SubagentPersonasCommand) Execute(args []string, chatAgent *agent.Agent) error {
	configManager := chatAgent.GetConfigManager()
	config := configManager.GetConfig()

	return showAllPersonas(config)
}

// SubagentPersonaCommand implements the /subagent-persona command (show/configure a specific persona)
type SubagentPersonaCommand struct{}

// Name returns the command name
func (s *SubagentPersonaCommand) Name() string {
	return "subagent-persona"
}

// Description returns the command description
func (s *SubagentPersonaCommand) Description() string {
	return "Show or configure a specific subagent persona"
}

// Execute runs the subagent persona command
func (s *SubagentPersonaCommand) Execute(args []string, chatAgent *agent.Agent) error {
	configManager := chatAgent.GetConfigManager()
	config := configManager.GetConfig()

	// If no arguments, show list of personas (same as /subagent-personas)
	if len(args) == 0 {
		return showAllPersonas(config)
	}

	// First argument is the persona name
	personaName := args[0]

	// If only persona name provided, show details for that persona
	if len(args) == 1 {
		return showPersonaDetails(personaName, config)
	}

	// Second argument is the action (enable, disable, provider, model)
	action := args[1]

	switch action {
	case "enable":
		return setPersonaEnabled(personaName, true, configManager)
	case "disable":
		return setPersonaEnabled(personaName, false, configManager)
	case "provider":
		if len(args) < 3 {
			return fmt.Errorf("usage: /subagent-persona %s provider <provider>", personaName)
		}
		return setPersonaProvider(personaName, args[2], configManager)
	case "model":
		if len(args) < 3 {
			return fmt.Errorf("usage: /subagent-persona %s model <model>", personaName)
		}
		return setPersonaModel(personaName, args[2], configManager)
	default:
		return fmt.Errorf("unknown action: %s\n\nValid actions: enable, disable, provider, model", action)
	}
}

// showAllPersonas displays all available subagent personas
func showAllPersonas(config *configuration.Config) error {
	fmt.Println("\n🎭 Subagent Personas:")
	fmt.Println("====================")

	if config.SubagentTypes == nil || len(config.SubagentTypes) == 0 {
		fmt.Println("⚠️  No personas configured")
		return nil
	}

	for _, persona := range config.SubagentTypes {
		status := "✅ Enabled"
		if !persona.Enabled {
			status = "❌ Disabled"
		}
		fmt.Printf("\n%s **%s** (%s)\n", status, persona.Name, persona.ID)
		fmt.Printf("   %s\n", persona.Description)

		// Show configuration if different from defaults
		provider := persona.Provider
		model := persona.Model
		if provider == "" {
			provider = "<default>"
		}
		if model == "" {
			model = "<default>"
		}
		fmt.Printf("   📦 Provider: %s | 🤖 Model: %s\n", provider, model)
	}

	fmt.Println("\n💡 Usage:")
	fmt.Println("  /subagent-personas                    - List all personas")
	fmt.Println("  /subagent-persona <name>              - Show persona details")
	fmt.Println("  /subagent-persona <name> enable       - Enable a persona")
	fmt.Println("  /subagent-persona <name> disable      - Disable a persona")
	fmt.Println("  /subagent-persona <name> provider <p> - Set provider for persona")
	fmt.Println("  /subagent-persona <name> model <m>     - Set model for persona")
	fmt.Println()
	fmt.Println("💡 Use personas with: run_subagent tool with persona parameter")
	fmt.Println("   Example: {\"tool\": \"run_subagent\", \"prompt\": \"...\", \"persona\": \"debugger\"}")

	return nil
}

// showPersonaDetails displays detailed information about a specific persona
func showPersonaDetails(personaName string, config *configuration.Config) error {
	// Find the persona (case-insensitive)
	var persona *configuration.SubagentType
	var personaID string

	for id, p := range config.SubagentTypes {
		if strings.EqualFold(p.Name, personaName) || strings.EqualFold(id, personaName) {
			persona = &p
			personaID = id
			break
		}
	}

	if persona == nil {
		return fmt.Errorf("persona not found: %s\n\nAvailable personas: %s",
			personaName, getAvailablePersonaNames(config))
	}

	fmt.Printf("\n🎭 **%s** (%s)\n", persona.Name, personaID)
	fmt.Println(strings.Repeat("=", len(persona.Name)+len(personaID)+5))
	fmt.Printf("📝 Description: %s\n", persona.Description)

	status := "✅ Enabled"
	if !persona.Enabled {
		status = "❌ Disabled"
	}
	fmt.Printf("🚦 Status: %s\n", status)

	// Configuration
	provider := persona.Provider
	if provider == "" {
		provider = "<default> (uses subagent-provider setting)"
	}
	model := persona.Model
	if model == "" {
		model = "<default> (uses subagent-model setting)"
	}

	fmt.Printf("\n⚙️  Configuration:\n")
	fmt.Printf("   📦 Provider: %s\n", provider)
	fmt.Printf("   🤖 Model: %s\n", model)
	fmt.Printf("   📄 System Prompt: %s\n", persona.SystemPrompt)

	fmt.Println("\n💡 Configuration Commands:")
	fmt.Printf("   /subagent-persona %s provider <provider>  - Set provider\n", persona.ID)
	fmt.Printf("   /subagent-persona %s model <model>         - Set model\n", persona.ID)
	fmt.Printf("   /subagent-persona %s enable               - Enable persona\n", persona.ID)
	fmt.Printf("   /subagent-persona %s disable              - Disable persona\n", persona.ID)

	// Check if system prompt file exists
	if persona.SystemPrompt != "" {
		if _, err := os.Stat(persona.SystemPrompt); os.IsNotExist(err) {
			fmt.Printf("\n⚠️  Warning: System prompt file not found: %s\n", persona.SystemPrompt)
		}
	}

	return nil
}

// setPersonaEnabled enables or disables a persona
func setPersonaEnabled(personaName string, enabled bool, configManager *configuration.Manager) error {
	personaID, personaNameDisplay, err := findPersonaID(personaName, configManager.GetConfig())
	if err != nil {
		return err
	}

	if err := configManager.UpdateConfig(func(c *configuration.Config) error {
		persona := c.SubagentTypes[personaID]
		persona.Enabled = enabled
		c.SubagentTypes[personaID] = persona
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	action := "enabled"
	if !enabled {
		action = "disabled"
	}

	fmt.Printf("\n✅ Persona '%s' (%s) %s\n", personaNameDisplay, personaID, action)
	return nil
}

// setPersonaProvider sets the provider for a persona
func setPersonaProvider(personaName, provider string, configManager *configuration.Manager) error {
	personaID, personaNameDisplay, err := findPersonaID(personaName, configManager.GetConfig())
	if err != nil {
		return err
	}

	if err := validateProvider(provider, configManager); err != nil {
		return err
	}
	if err := configManager.UpdateConfig(func(c *configuration.Config) error {
		persona := c.SubagentTypes[personaID]
		persona.Provider = provider
		c.SubagentTypes[personaID] = persona
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n✅ Provider for persona '%s' (%s) set to: %s\n",
		personaNameDisplay, personaID, provider)
	fmt.Println("💡 This persona will now use the specified provider instead of the default subagent provider.")
	return nil
}

// setPersonaModel sets the model for a persona
func setPersonaModel(personaName, model string, configManager *configuration.Manager) error {
	personaID, personaNameDisplay, err := findPersonaID(personaName, configManager.GetConfig())
	if err != nil {
		return err
	}

	if err := configManager.UpdateConfig(func(c *configuration.Config) error {
		persona := c.SubagentTypes[personaID]
		persona.Model = model
		c.SubagentTypes[personaID] = persona
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n✅ Model for persona '%s' (%s) set to: %s\n",
		personaNameDisplay, personaID, model)
	fmt.Println("💡 This persona will now use the specified model instead of the default subagent model.")
	return nil
}

// findPersonaID looks up a persona by name or ID (case-insensitive), returning its ID and display name.
func findPersonaID(personaName string, config *configuration.Config) (string, string, error) {
	for id, p := range config.SubagentTypes {
		if strings.EqualFold(p.Name, personaName) || strings.EqualFold(id, personaName) {
			return id, p.Name, nil
		}
	}
	return "", "", fmt.Errorf("persona not found: %s\n\nAvailable personas: %s",
		personaName, getAvailablePersonaNames(config))
}

// getAvailablePersonaNames returns a comma-separated list of available persona names
func getAvailablePersonaNames(config *configuration.Config) string {
	if config.SubagentTypes == nil || len(config.SubagentTypes) == 0 {
		return "<none>"
	}

	var names []string
	for _, p := range config.SubagentTypes {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}
