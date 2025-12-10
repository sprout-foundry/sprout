package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/spf13/cobra"
)

var customModelCmd = &cobra.Command{
	Use:   "custom-model",
	Short: "Manage custom model providers",
	Long: `Manage custom model providers that extend ledit with additional AI services.
Use subcommands to add, remove, or list configured custom model providers.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var customModelAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new custom model provider interactively",
	Long: `Interactively add a new custom model provider configuration.
This will guide you through setting up a custom AI provider with URL, model name, context size, and API key.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runCustomModelAdd(); err != nil {
			fmt.Fprintf(os.Stderr, "Error adding custom model provider: %v\n", err)
			os.Exit(1)
		}
	},
}

var customModelRemoveCmd = &cobra.Command{
	Use:   "remove [provider-name]",
	Short: "Remove a custom model provider",
	Long:  `Remove a custom model provider from the configuration.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var providerName string
		if len(args) > 0 {
			providerName = args[0]
		}
		if err := runCustomModelRemove(providerName); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing custom model provider: %v\n", err)
			os.Exit(1)
		}
	},
}

var customModelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured custom model providers",
	Long:  `Display all configured custom model providers and their details.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runCustomModelList(); err != nil {
			fmt.Fprintf(os.Stderr, "Error listing custom model providers: %v\n", err)
			os.Exit(1)
		}
	},
}

func runCustomModelAdd() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Custom Model Provider Setup")
	fmt.Println("============================")
	fmt.Println()

	// Load existing config
	config, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize custom providers map if it doesn't exist
	if config.CustomProviders == nil {
		config.CustomProviders = make(map[string]configuration.CustomProviderConfig)
	}

	// Get provider name
	fmt.Print("Enter provider name (e.g., 'my-custom-llm'): ")
	providerName, _ := reader.ReadString('\n')
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return fmt.Errorf("provider name cannot be empty")
	}

	// Check if provider already exists
	if _, exists := config.CustomProviders[providerName]; exists {
		fmt.Printf("Provider '%s' already exists. Would you like to update it? (y/N): ", providerName)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			fmt.Println("Operation cancelled.")
			return nil
		}
	}

	// Get endpoint URL
	fmt.Print("Enter endpoint URL (e.g., 'https://api.example.com/v1/chat/completions'): ")
	endpoint, _ := reader.ReadString('\n')
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return fmt.Errorf("endpoint URL cannot be empty")
	}

	// Get model name
	fmt.Print("Enter model name (e.g., 'custom-llm-v1'): ")
	modelName, _ := reader.ReadString('\n')
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return fmt.Errorf("model name cannot be empty")
	}

	// Get context size
	fmt.Print("Enter max context size (tokens, e.g., 32000): ")
	contextSizeStr, _ := reader.ReadString('\n')
	contextSizeStr = strings.TrimSpace(contextSizeStr)
	contextSize, err := strconv.Atoi(contextSizeStr)
	if err != nil || contextSize <= 0 {
		return fmt.Errorf("invalid context size: must be a positive integer")
	}

	// Ask about API key
	fmt.Print("Does this provider require an API key? (y/N): ")
	apiKeyResponse, _ := reader.ReadString('\n')
	apiKeyResponse = strings.ToLower(strings.TrimSpace(apiKeyResponse))
	
	var apiKey, envVar string
	requiresAPIKey := apiKeyResponse == "y" || apiKeyResponse == "yes"
	
	if requiresAPIKey {
		fmt.Print("Enter API key (or leave empty to use environment variable): ")
		apiKey, _ = reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
		
		if apiKey == "" {
			fmt.Print("Enter environment variable name for API key (e.g., 'CUSTOM_API_KEY'): ")
			envVar, _ = reader.ReadString('\n')
			envVar = strings.TrimSpace(envVar)
			if envVar == "" {
				return fmt.Errorf("either API key or environment variable name must be provided")
			}
		}
	}

	// Create custom provider configuration
	customProvider := configuration.CustomProviderConfig{
		Name:        providerName,
		Endpoint:    endpoint,
		ModelName:   modelName,
		ContextSize: contextSize,
		RequiresAPIKey: requiresAPIKey,
	}

	if apiKey != "" {
		customProvider.APIKey = apiKey
	}
	if envVar != "" {
		customProvider.EnvVar = envVar
	}

	// Save to configuration
	config.CustomProviders[providerName] = customProvider

	// Add to provider models and priority if not already present
	if config.ProviderModels == nil {
		config.ProviderModels = make(map[string]string)
	}
	config.ProviderModels[providerName] = modelName

	// Add to provider priority if not already present
	found := false
	for _, existingProvider := range config.ProviderPriority {
		if existingProvider == providerName {
			found = true
			break
		}
	}
	if !found {
		config.ProviderPriority = append(config.ProviderPriority, providerName)
	}

	// Save configuration
	if err := config.Save(); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Custom model provider '%s' has been successfully added!\n", providerName)
	fmt.Printf("   Endpoint: %s\n", endpoint)
	fmt.Printf("   Model: %s\n", modelName)
	fmt.Printf("   Context Size: %d tokens\n", contextSize)
	if requiresAPIKey {
		if apiKey != "" {
			fmt.Printf("   API Key: [configured]\n")
		} else {
			fmt.Printf("   API Key: from environment variable '%s'\n", envVar)
		}
	}
	fmt.Println()
	fmt.Printf("You can now use this provider with: ledit agent --provider %s\n", providerName)
	fmt.Println()

	return nil
}

func runCustomModelRemove(providerName string) error {
	// Load existing config
	config, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if config.CustomProviders == nil {
		fmt.Println("No custom model providers configured.")
		return nil
	}

	// If no provider name provided, list them and ask to choose
	if providerName == "" {
		if len(config.CustomProviders) == 0 {
			fmt.Println("No custom model providers configured.")
			return nil
		}

		fmt.Println("Configured custom model providers:")
		var names []string
		for name := range config.CustomProviders {
			fmt.Printf("  - %s\n", name)
			names = append(names, name)
		}
		fmt.Println()

		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Enter provider name to remove: ")
		providerName, _ = reader.ReadString('\n')
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			return fmt.Errorf("provider name cannot be empty")
		}
	}

	// Check if provider exists
	provider, exists := config.CustomProviders[providerName]
	if !exists {
		return fmt.Errorf("custom model provider '%s' not found", providerName)
	}

	// Confirm removal
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Are you sure you want to remove provider '%s' (model: %s)? (y/N): ", providerName, provider.ModelName)
	response, _ := reader.ReadString('\n')
	response = strings.ToLower(strings.TrimSpace(response))
	if response != "y" && response != "yes" {
		fmt.Println("Operation cancelled.")
		return nil
	}

	// Remove from custom providers
	delete(config.CustomProviders, providerName)

	// Remove from provider models
	delete(config.ProviderModels, providerName)

	// Remove from provider priority
	var newPriority []string
	for _, p := range config.ProviderPriority {
		if p != providerName {
			newPriority = append(newPriority, p)
		}
	}
	config.ProviderPriority = newPriority

	// Save configuration
	if err := config.Save(); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	fmt.Printf("✅ Custom model provider '%s' has been removed.\n", providerName)
	return nil
}

func runCustomModelList() error {
	// Load existing config
	config, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if config.CustomProviders == nil || len(config.CustomProviders) == 0 {
		fmt.Println("No custom model providers configured.")
		fmt.Println("Use 'ledit custom-model add' to add a new provider.")
		return nil
	}

	fmt.Println("Custom Model Providers")
	fmt.Println("======================")
	fmt.Println()

	for name, provider := range config.CustomProviders {
		fmt.Printf("Provider: %s\n", name)
		fmt.Printf("  Endpoint: %s\n", provider.Endpoint)
		fmt.Printf("  Model: %s\n", provider.ModelName)
		fmt.Printf("  Context Size: %d tokens\n", provider.ContextSize)
		fmt.Printf("  API Key Required: %t\n", provider.RequiresAPIKey)
		if provider.RequiresAPIKey {
			if provider.APIKey != "" {
				fmt.Printf("  API Key: [configured]\n")
			} else if provider.EnvVar != "" {
				fmt.Printf("  API Key: from environment variable '%s'\n", provider.EnvVar)
			}
		}
		fmt.Println()
	}

	fmt.Println("Usage:")
	fmt.Println("  ledit agent --provider <provider-name>")
	fmt.Println()

	return nil
}

func init() {
	customModelCmd.AddCommand(customModelAddCmd)
	customModelCmd.AddCommand(customModelRemoveCmd)
	customModelCmd.AddCommand(customModelListCmd)
}