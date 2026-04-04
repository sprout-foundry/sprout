package commands

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
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

	fmt.Println("\n[tool] Subagent Configuration:")
	fmt.Println("========================")
	fmt.Printf("[pkg] **Provider**: %s\n", formatValue(provider))
	fmt.Printf("[bot] **Model**: %s\n", formatValue(model))
	fmt.Println()
	fmt.Println("[i] Usage:")
	fmt.Println("  /subagent-provider <provider>  - Set subagent provider")
	fmt.Println("  /subagent-model <model>        - Set subagent model")
	fmt.Println()
	fmt.Println("[i] Subagents will use these settings instead of the parent agent's configuration.")
	fmt.Println("[i] Leave empty to use the parent agent's provider/model.")
	return nil
}

// setProvider sets the subagent provider
func (s *SubagentConfigCommand) setProvider(provider string, configManager *configuration.Manager) error {
	if err := validateProvider(provider, configManager); err != nil {
		return err
	}

	if err := configManager.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider(provider)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n[OK] Subagent provider set to: %s\n", provider)
	fmt.Println("[i] Subagents will now use this provider for all executions.")
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

	fmt.Printf("\n[OK] Subagent model set to: %s\n", model)
	fmt.Println("[i] Subagents will now use this model for all executions.")
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
	return fmt.Errorf("invalid provider: %s\n\nAvailable providers: %v", provider, available)
}

// formatValue formats a config value for display
func formatValue(value string) string {
	if value == "" {
		return "<default> (uses parent agent's setting)"
	}
	return value
}
