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
	config := configManager.GetConfig()

	// If no arguments, show current status
	if len(args) == 0 {
		return s.showStatus(config)
	}

	// Set the value
	value := args[0]
	if s.configType == "provider" {
		return s.setProvider(value, config, configManager)
	}
	return s.setModel(value, config, configManager)
}

// showStatus displays current subagent configuration
func (s *SubagentConfigCommand) showStatus(config *configuration.Config) error {
	provider := config.GetSubagentProvider()
	model := config.GetSubagentModel()

	fmt.Println("\n🔧 Subagent Configuration:")
	fmt.Println("========================")
	fmt.Printf("📦 **Provider**: %s\n", formatValue(provider))
	fmt.Printf("🤖 **Model**: %s\n", formatValue(model))
	fmt.Println()
	fmt.Println("💡 Usage:")
	fmt.Println("  /subagent-provider <provider>  - Set subagent provider")
	fmt.Println("  /subagent-model <model>        - Set subagent model")
	fmt.Println()
	fmt.Println("💡 Subagents will use these settings instead of the parent agent's configuration.")
	fmt.Println("💡 Leave empty to use the parent agent's provider/model.")
	return nil
}

// setProvider sets the subagent provider
func (s *SubagentConfigCommand) setProvider(provider string, config *configuration.Config, configManager *configuration.Manager) error {
	// Validate provider exists by converting to ClientType
	providerType, err := configManager.MapStringToClientType(provider)
	if err != nil {
		return fmt.Errorf("invalid provider: %s\n\n%v", provider, err)
	}

	// Check if it's a real provider (not the error type)
	available := configManager.GetAvailableProviders()
	isValid := false
	for _, p := range available {
		if p == providerType {
			isValid = true
			break
		}
	}

	if !isValid {
		return fmt.Errorf("invalid provider: %s\n\nAvailable providers: %v", provider, available)
	}

	config.SetSubagentProvider(provider)
	if err := configManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n✅ Subagent provider set to: %s\n", provider)
	fmt.Println("💡 Subagents will now use this provider for all executions.")
	return nil
}

// setModel sets the subagent model
func (s *SubagentConfigCommand) setModel(model string, config *configuration.Config, configManager *configuration.Manager) error {
	config.SetSubagentModel(model)
	if err := configManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n✅ Subagent model set to: %s\n", model)
	fmt.Println("💡 Subagents will now use this model for all executions.")
	return nil
}

// formatValue formats a config value for display
func formatValue(value string) string {
	if value == "" {
		return "<default> (uses parent agent's setting)"
	}
	return value
}
