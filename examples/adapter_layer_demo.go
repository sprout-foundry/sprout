package main

import (
	"context"
	"fmt"
	"log"

	"github.com/alantheprice/ledit/pkg/adapters"
	"github.com/alantheprice/ledit/pkg/boundaries"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/providers/llm"
)

func main() {
	fmt.Println("=== Adapter Layer Demo ===")

	// Create test configuration
	cfg := &config.Config{
		LLMProvider:       "openai",
		Model:             "gpt-3.5-turbo",
		OpenAIAPIKey:      "test-key-placeholder",
		MaxFilesInContext: 10,
		MaxFileSize:       1024 * 1024,
		SkipPrompt:        true,
		Debug:             true,
	}

	// Test 1: Adapter Factory Creation
	fmt.Println("\n1. Testing Adapter Factory Creation...")
	factory, err := adapters.NewAdapterFactory(cfg)
	if err != nil {
		log.Fatalf("Failed to create adapter factory: %v", err)
	}
	fmt.Println("✓ Adapter factory created successfully")

	// Test 2: LLM Provider Registry
	fmt.Println("\n2. Testing LLM Provider Registry...")
	registry := llm.NewRegistry()
	
	// Register providers
	if err := registry.RegisterOpenAI(); err != nil {
		fmt.Printf("Warning: Failed to register OpenAI: %v\n", err)
	} else {
		fmt.Println("✓ OpenAI provider registered")
	}
	
	if err := registry.RegisterOllama(); err != nil {
		fmt.Printf("Warning: Failed to register Ollama: %v\n", err)
	} else {
		fmt.Println("✓ Ollama provider registered")
	}

	// Get provider capabilities
	providers := registry.GetAvailableProviders()
	fmt.Printf("✓ Available providers: %v\n", providers)

	// Test 3: Enhanced Container with Adapter Layer
	fmt.Println("\n3. Testing Enhanced Container with Adapter Layer...")
	container := boundaries.NewEnhancedContainer(cfg)
	
	// Initialize container
	ctx := context.Background()
	if err := container.Start(ctx); err != nil {
		log.Fatalf("Failed to start container: %v", err)
	}
	fmt.Println("✓ Enhanced container started successfully")

	// Test domain services
	todoService := container.GetTodoService()
	if todoService != nil {
		fmt.Println("✓ Todo service available")
	} else {
		fmt.Println("⚠ Todo service not available")
	}

	agentWorkflow := container.GetAgentWorkflow()
	if agentWorkflow != nil {
		fmt.Println("✓ Agent workflow available")
	} else {
		fmt.Println("⚠ Agent workflow not available")
	}

	// Test enhanced providers
	llmProvider := container.GetLLMProviderNew()
	if llmProvider != nil {
		fmt.Println("✓ Enhanced LLM provider available")
	} else {
		fmt.Println("⚠ Enhanced LLM provider not available")
	}

	configProvider := container.GetConfigProvider()
	if configProvider != nil {
		fmt.Println("✓ Enhanced config provider available")
		
		// Test config retrieval
		if agentConfig := configProvider.GetAgentConfig(); agentConfig != nil {
			fmt.Printf("✓ Agent config retrieved: retries=%d, validation=%t\n", 
				agentConfig.MaxRetries, agentConfig.EnableValidation)
		}
		
		if uiConfig := configProvider.GetUIConfig(); uiConfig != nil {
			fmt.Printf("✓ UI config retrieved: skip_prompts=%t, verbose=%t\n", 
				uiConfig.SkipPrompts, uiConfig.VerboseLogging)
		}
	} else {
		fmt.Println("⚠ Enhanced config provider not available")
	}

	workspaceAnalyzer := container.GetWorkspaceAnalyzer()
	if workspaceAnalyzer != nil {
		fmt.Println("✓ Workspace analyzer available")
	} else {
		fmt.Println("⚠ Workspace analyzer not available")
	}

	// Test 4: Service Registry
	fmt.Println("\n4. Testing Service Registry...")
	services := container.ListServices()
	fmt.Printf("✓ Registered services count: %d\n", len(services))
	for _, service := range services {
		fmt.Printf("  - %s: %v\n", service.Name, service.Status)
	}

	// Test 5: Container Health Check
	fmt.Println("\n5. Testing Container Health Check...")
	health, err := container.HealthCheck(ctx)
	if err != nil {
		fmt.Printf("⚠ Health check failed: %v\n", err)
	} else {
		fmt.Printf("✓ Container health: %s\n", health.Status)
		for service, status := range health.Services {
			fmt.Printf("  - %s: %s\n", service, status)
		}
	}

	// Test 6: Migration Support
	fmt.Println("\n6. Testing Migration Support...")
	migration := adapters.NewMigrationSupport(factory)
	flags := migration.GetMigrationFlags()
	fmt.Printf("✓ Migration flags: domain=%t, container=%t, adapter=%t, config=%t\n",
		flags.UseDomainServices, flags.UseEnhancedContainer, 
		flags.UseAdapterLayer, flags.UseLayeredConfig)

	// Cleanup
	fmt.Println("\n7. Cleanup...")
	if err := container.Stop(ctx); err != nil {
		fmt.Printf("⚠ Failed to stop container: %v\n", err)
	} else {
		fmt.Println("✓ Container stopped successfully")
	}

	fmt.Println("\n=== Adapter Layer Demo Completed Successfully ===")
}