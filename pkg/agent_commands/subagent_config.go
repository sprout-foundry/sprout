package commands

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// SubagentConfigCommand implements the /subagent-provider and /subagent-model commands
type SubagentConfigCommand struct {
	configType string // "provider" or "model"
}

// Name returns the command name
func (s *SubagentConfigCommand) Name() string {
	if s.configType == "provider" {
		return "subagent-provider"
	}
	return "subagent-model"
}

// Description returns the command description
func (s *SubagentConfigCommand) Description() string {
	if s.configType == "provider" {
		return "Set or show subagent provider configuration"
	}
	return "Set or show subagent model configuration"
}

// Execute runs the subagent config command
func (s *SubagentConfigCommand) Execute(args []string, chatAgent *agent.Agent) error {
	configManager := chatAgent.GetConfigManager()

	// If no arguments, show current status
	if len(args) == 0 {
		return s.showStatus(configManager.GetConfig())
	}

	// Set the value
	value := args[0]
	if s.configType == "provider" {
		return s.setProvider(value, configManager)
	}
	return s.setModel(value, configManager)
}

// showStatus displays current subagent configuration
func (s *SubagentConfigCommand) showStatus(config *configuration.Config) error {
	provider := config.GetSubagentProvider()
	model := config.GetSubagentModel()

	fmt.Println()
	console.GlyphInfo.Print("Subagent Configuration:")
	fmt.Printf("Provider: %s\n", formatValue(provider))
	fmt.Printf("Model:    %s\n", formatValue(model))
	fmt.Println()
	console.GlyphInfo.Print("Usage:")
	fmt.Println("  /subagent-provider <provider>  - Set subagent provider")
	fmt.Println("  /subagent-model <model>        - Set subagent model")
	fmt.Println()
	console.GlyphInfo.Print("Subagents will use these settings instead of the parent agent's configuration.")
	console.GlyphInfo.Print("Leave empty to use the parent agent's provider/model.")
	return nil
}

// setProvider sets the subagent provider
func (s *SubagentConfigCommand) setProvider(provider string, configManager *configuration.Manager) error {
	if err := validateProvider(provider, configManager); err != nil {
		return fmt.Errorf("setProvider: %w", err)
	}

	if err := configManager.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider(provider)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	console.GlyphSuccess.Printf("Subagent provider set to: %s", provider)
	console.GlyphInfo.Print("Subagents will now use this provider for all executions.")
	return nil
}

// setModel sets the subagent model
func (s *SubagentConfigCommand) setModel(model string, configManager *configuration.Manager) error {
	if err := configManager.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentModel(model)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	console.GlyphSuccess.Printf("Subagent model set to: %s", model)
	console.GlyphInfo.Print("Subagents will now use this model for all executions.")
	return nil
}

// validateProvider checks that a provider name is valid and available.
func validateProvider(provider string, configManager *configuration.Manager) error {
	providerType, err := configManager.MapStringToClientType(provider)
	if err != nil {
		return fmt.Errorf("invalid provider: %s\n\n%w", provider, err)
	}

	available := configManager.GetAvailableProviders()
	for _, p := range available {
		if p == providerType {
			return nil
		}
	}
	return fmt.Errorf("invalid provider: %s\n\nAvailable providers: %s", provider, available)
}

// formatValue formats a config value for display
func formatValue(value string) string {
	if value == "" {
		return "<default> (uses parent agent's setting)"
	}
	return value
}
