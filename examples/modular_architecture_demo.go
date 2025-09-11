package main

import (
	"context"
	"fmt"
	"log"

	"github.com/alantheprice/ledit/pkg/interfaces/types"
	"github.com/alantheprice/ledit/pkg/providers"
	"github.com/alantheprice/ledit/pkg/providers/config"
	"github.com/alantheprice/ledit/pkg/providers/llm"
	"github.com/alantheprice/ledit/pkg/providers/prompts"
)

// This example demonstrates the new modular architecture
func main() {
	demoModularArchitecture()
}

func demoModularArchitecture() {
	fmt.Println("🚀 Ledit Modular Architecture Demo")
	fmt.Println("==================================")

	// Initialize all default providers
	providers.MustRegisterDefaultProviders()

	// Get the global registry
	registry := llm.GetGlobalRegistry()

	// List available providers
	fmt.Println("\n📦 Available LLM Providers:")
	availableProviders := registry.ListProviders()
	for _, provider := range availableProviders {
		fmt.Printf("  - %s\n", provider)
	}

	// Create a factory for easy provider creation
	factory := llm.NewGlobalFactory()

	// Demo provider capabilities
	fmt.Println("\n🔍 Provider Capabilities:")
	for _, providerName := range availableProviders {
		capabilities, err := factory.GetProviderCapabilities(providerName)
		if err != nil {
			log.Printf("Error getting capabilities for %s: %v", providerName, err)
			continue
		}

		fmt.Printf("  %s:\n", capabilities.Name)
		fmt.Printf("    - Tools: %t\n", capabilities.SupportsTools)
		fmt.Printf("    - Images: %t\n", capabilities.SupportsImages)
		fmt.Printf("    - Streaming: %t\n", capabilities.SupportsStream)
		fmt.Printf("    - Max Tokens: %d\n", capabilities.MaxTokens)
		fmt.Printf("    - Models: %v\n", capabilities.SupportedModels)
	}

	// Demo configuration system
	fmt.Println("\n⚙️  Configuration System Demo:")
	configProvider := config.NewLayeredProvider()

	// Get various config sections
	agentConfig := configProvider.GetAgentConfig()
	fmt.Printf("  Agent Config - Max Retries: %d\n", agentConfig.MaxRetries)

	editorConfig := configProvider.GetEditorConfig()
	fmt.Printf("  Editor Config - Auto Format: %t\n", editorConfig.AutoFormat)

	uiConfig := configProvider.GetUIConfig()
	fmt.Printf("  UI Config - Color Output: %t\n", uiConfig.ColorOutput)

	// Demo prompt system
	fmt.Println("\n📝 Prompt System Demo:")
	promptManager := prompts.NewManager("")

	// Create a sample prompt
	samplePrompt := "Hello {{.Name}}, welcome to {{.System}}!"
	err := promptManager.SavePrompt("welcome", samplePrompt)
	if err != nil {
		log.Printf("Error saving prompt: %v", err)
	} else {
		fmt.Println("  ✓ Saved sample prompt template")
	}

	// Load and render with variables
	variables := map[string]string{
		"Name":   "Developer",
		"System": "Ledit Modular Architecture",
	}

	rendered, err := promptManager.LoadPromptWithVariables("welcome", variables)
	if err != nil {
		log.Printf("Error rendering prompt: %v", err)
	} else {
		fmt.Printf("  Rendered prompt: %s\n", rendered)
	}

	// List available prompts
	prompts := promptManager.ListPrompts()
	fmt.Printf("  Available prompts: %v\n", prompts)

	// Demo provider creation and health check
	fmt.Println("\n🏥 Provider Health Check Demo:")

	// Create a sample OpenAI provider config
	openaiConfig := &types.ProviderConfig{
		Name:    "openai",
		Model:   "gpt-3.5-turbo",
		APIKey:  "fake-api-key-for-demo",
		BaseURL: "https://api.openai.com/v1",
		Enabled: true,
		Timeout: 30,
	}

	// Validate configuration
	if err := factory.ValidateProviderConfig(openaiConfig); err != nil {
		fmt.Printf("  ❌ OpenAI config validation failed: %v\n", err)
	} else {
		fmt.Println("  ✓ OpenAI config validation passed")
	}

	// Try to create provider (will fail without real API key, but shows the flow)
	provider, err := factory.CreateProvider(openaiConfig)
	if err != nil {
		fmt.Printf("  ❌ Provider creation failed (expected): %v\n", err)
	} else {
		fmt.Printf("  ✓ Provider '%s' created successfully\n", provider.GetName())

		// Check health
		ctx := context.Background()
		if err := provider.IsAvailable(ctx); err != nil {
			fmt.Printf("  ❌ Provider health check failed: %v\n", err)
		} else {
			fmt.Println("  ✓ Provider is healthy")
		}
	}

	fmt.Println("\n✨ Demo completed! The modular architecture is now ready for use.")
	fmt.Println("\nKey Benefits:")
	fmt.Println("  • Clean separation of concerns")
	fmt.Println("  • Easy to add new providers")
	fmt.Println("  • Configurable and extensible")
	fmt.Println("  • Testable components")
	fmt.Println("  • Backward compatibility support")
}
