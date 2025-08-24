package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/config/layered"
)

func main() {
	fmt.Println("=== Layered Configuration System Demo ===")

	// Test 1: Basic layered configuration setup
	fmt.Println("\n1. Testing Basic Layered Configuration Setup...")
	factory := layered.NewConfigurationFactory()
	
	// Create standard setup (defaults + global + project + environment)
	_, err := factory.CreateStandardSetup()
	if err != nil {
		log.Fatalf("Failed to create standard config setup: %v", err)
	}
	fmt.Println("✓ Standard layered configuration created")

	// Test 2: Configuration merging and priority
	fmt.Println("\n2. Testing Configuration Merging...")
	
	// Create temp directory for test configs
	tempDir, err := os.MkdirTemp("", "ledit-config-test")
	if err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	fmt.Printf("✓ Created test directory: %s\n", tempDir)

	// Create test configuration files
	createTestConfigFiles(tempDir)

	// Create custom layered provider with test files
	testProvider := layered.NewLayeredConfigProvider()
	
	// Add sources in priority order (lowest to highest)
	defaultsSource := layered.NewDefaultsConfigSource("defaults", 0)
	testProvider.AddSource(defaultsSource)
	fmt.Println("✓ Added defaults source")

	globalConfigPath := filepath.Join(tempDir, "global.json")
	globalSource := layered.NewFileConfigSource(globalConfigPath, "global", 10, false)
	testProvider.AddSource(globalSource)
	fmt.Println("✓ Added global config source")

	projectConfigPath := filepath.Join(tempDir, "project.json")
	projectSource := layered.NewFileConfigSource(projectConfigPath, "project", 20, false)
	testProvider.AddSource(projectSource)
	fmt.Println("✓ Added project config source")

	// Set environment variables for testing
	os.Setenv("LEDIT_TEMPERATURE", "0.9")
	os.Setenv("LEDIT_VERBOSE_LOGGING", "true")
	os.Setenv("LEDIT_SKIP_PROMPTS", "true")
	defer func() {
		os.Unsetenv("LEDIT_TEMPERATURE")
		os.Unsetenv("LEDIT_VERBOSE_LOGGING")
		os.Unsetenv("LEDIT_SKIP_PROMPTS")
	}()

	envSource := layered.NewEnvironmentConfigSource("LEDIT_", "environment", 30)
	testProvider.AddSource(envSource)
	fmt.Println("✓ Added environment config source")

	// Test merged configuration
	mergedConfig := testProvider.GetMergedConfig()
	if mergedConfig == nil {
		log.Fatal("Failed to get merged configuration")
	}
	fmt.Println("✓ Successfully merged configuration")

	// Display merged results
	displayConfigurationResults(mergedConfig)

	// Test 3: Configuration validation
	fmt.Println("\n3. Testing Configuration Validation...")
	validator := layered.CreateStandardValidator()
	validationResult := validator.ValidateConfig(mergedConfig)
	
	fmt.Printf("✓ Validation completed - Valid: %t, Errors: %d, Warnings: %d\n", 
		validationResult.IsValid(), len(validationResult.Errors), len(validationResult.Warnings))

	if len(validationResult.Errors) > 0 {
		fmt.Println("  Validation Errors:")
		for _, err := range validationResult.Errors {
			fmt.Printf("    - %s: %s\n", err.Field, err.Message)
		}
	}

	if len(validationResult.Warnings) > 0 {
		fmt.Println("  Validation Warnings:")
		for _, warn := range validationResult.Warnings {
			fmt.Printf("    - %s: %s\n", warn.Field, warn.Message)
		}
	}

	// Test 4: Validated layered config provider
	fmt.Println("\n4. Testing Validated Layered Config Provider...")
	validatedProvider := layered.NewValidatedLayeredConfigProvider(testProvider)
	
	// Test getting specific configurations
	agentConfig := validatedProvider.GetAgentConfig()
	fmt.Printf("✓ Agent config - MaxRetries: %d, EnableValidation: %t\n", 
		agentConfig.MaxRetries, agentConfig.EnableValidation)

	uiConfig := validatedProvider.GetUIConfig()
	fmt.Printf("✓ UI config - SkipPrompts: %t, VerboseLogging: %t, ColorOutput: %t\n", 
		uiConfig.SkipPrompts, uiConfig.VerboseLogging, uiConfig.ColorOutput)

	securityConfig := validatedProvider.GetSecurityConfig()
	fmt.Printf("✓ Security config - CredentialScanning: %t, RequireConfirmation: %t\n", 
		securityConfig.EnableCredentialScanning, securityConfig.RequireConfirmation)

	// Test provider configurations
	fmt.Println("\n5. Testing Provider Configurations...")
	
	providers := []string{"openai", "gemini", "ollama"}
	for _, providerName := range providers {
		providerConfig, err := validatedProvider.GetProviderConfig(providerName)
		if err != nil {
			fmt.Printf("⚠ Error getting %s config: %v\n", providerName, err)
			continue
		}
		fmt.Printf("✓ %s config - Model: %s, Temperature: %.2f, Timeout: %ds\n", 
			providerName, providerConfig.Model, providerConfig.Temperature, providerConfig.Timeout)
	}

	// Test 6: Configuration watching (simplified demo)
	fmt.Println("\n6. Testing Configuration Watching...")
	watchedProvider := layered.NewWatchedLayeredConfigProvider(validatedProvider)

	// Add config files to watch
	watchedProvider.WatchConfigFile(globalConfigPath)
	watchedProvider.WatchConfigFile(projectConfigPath)
	fmt.Println("✓ Added config files to watcher")

	// Test 7: Configuration manager
	fmt.Println("\n7. Testing Configuration Manager...")
	manager, err := layered.NewConfigurationManager()
	if err != nil {
		log.Printf("Warning: Could not create configuration manager: %v", err)
	} else {
		if err := manager.Start(); err != nil {
			log.Printf("Warning: Could not start configuration manager: %v", err)
		} else {
			fmt.Println("✓ Configuration manager started")
			
			// Get current config through manager
			currentConfig := manager.GetConfig()
			if currentConfig != nil {
				fmt.Println("✓ Retrieved configuration through manager")
			}

			// Validate current config
			validation := manager.ValidateCurrentConfig()
			fmt.Printf("✓ Manager validation - Valid: %t\n", validation.IsValid())

			// Stop manager
			manager.Stop()
			fmt.Println("✓ Configuration manager stopped")
		}
	}

	fmt.Println("\n=== Layered Configuration System Demo Completed Successfully ===")
	fmt.Println("\nKey Features Demonstrated:")
	fmt.Println("- ✅ Multiple configuration sources (defaults, files, environment)")
	fmt.Println("- ✅ Priority-based configuration merging")
	fmt.Println("- ✅ Comprehensive configuration validation")
	fmt.Println("- ✅ Type-safe configuration access")
	fmt.Println("- ✅ File watching capabilities")
	fmt.Println("- ✅ Centralized configuration management")
}

// createTestConfigFiles creates test configuration files
func createTestConfigFiles(tempDir string) {
	// Global config - sets base LLM settings
	globalConfig := &config.Config{
		LLM: &config.LLMConfig{
			EditingModel:       "deepinfra:google/gemini-2.5-flash",
			SummaryModel:       "deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo",
			OrchestrationModel: "deepinfra:moonshotai/Kimi-K2-Instruct",
			Temperature:        0.7,
			MaxTokens:          2000,
			DefaultTimeoutSecs: 120,
		},
		UI: &config.UIConfig{
			PreapplyReview:   false,
			JsonLogs:         false,
			HealthChecks:     true,
			TelemetryEnabled: false,
			TelemetryFile:    ".ledit/telemetry.json",
			TrackWithGit:     true,
			StagedEdits:      false,
		},
		Security: &config.SecurityConfig{
			EnableSecurityChecks: true,
			ShellAllowlist:       []string{"git", "go", "npm"},
			AllowedCommands:      []string{"git", "go", "npm"},
			RequireApproval:      false,
		},
	}

	writeConfigToFile(filepath.Join(tempDir, "global.json"), globalConfig)

	// Project config - overrides some settings
	projectConfig := &config.Config{
		LLM: &config.LLMConfig{
			EditingModel: "gpt-4", // Override editing model
			Temperature:  0.5,     // Override temperature
		},
		Agent: &config.AgentConfig{
			OrchestrationMaxAttempts: 5,
			PolicyVariant:            "aggressive",
			AutoGenerateTests:        true,
			DryRun:                   false,
		},
		Performance: &config.PerformanceConfig{
			MaxConcurrentRequests: 10,
			RequestDelayMs:        200,
			EmbeddingBatchSize:    50,
		},
	}

	writeConfigToFile(filepath.Join(tempDir, "project.json"), projectConfig)
	fmt.Printf("✓ Created test config files in %s\n", tempDir)
}

// writeConfigToFile writes a config object to a JSON file
func writeConfigToFile(filePath string, cfg *config.Config) {
	// For simplicity, create minimal JSON files
	globalJSON := `{
	"llm": {
		"editing_model": "deepinfra:google/gemini-2.5-flash",
		"temperature": 0.7,
		"max_tokens": 2000
	},
	"ui": {
		"color_output": true,
		"verbose_logging": false
	},
	"security": {
		"enable_credential_scanning": true
	}
}`

	projectJSON := `{
	"llm": {
		"editing_model": "gpt-4",
		"temperature": 0.5
	},
	"agent": {
		"orchestration_max_attempts": 5,
		"auto_generate_tests": true
	}
}`

	var content string
	if filepath.Base(filePath) == "global.json" {
		content = globalJSON
	} else {
		content = projectJSON
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		log.Printf("Warning: Could not write config file %s: %v", filePath, err)
	}
}

// displayConfigurationResults shows the merged configuration results
func displayConfigurationResults(cfg *config.Config) {
	fmt.Println("\n📋 Merged Configuration Results:")
	
	if cfg.LLM != nil {
		fmt.Printf("  LLM Configuration:\n")
		fmt.Printf("    - Editing Model: %s\n", cfg.LLM.EditingModel)
		fmt.Printf("    - Summary Model: %s\n", cfg.LLM.SummaryModel)
		fmt.Printf("    - Temperature: %.2f\n", cfg.LLM.Temperature)
		fmt.Printf("    - Max Tokens: %d\n", cfg.LLM.MaxTokens)
		fmt.Printf("    - Timeout: %ds\n", cfg.LLM.DefaultTimeoutSecs)
	}

	if cfg.UI != nil {
		fmt.Printf("  UI Configuration:\n")
		fmt.Printf("    - Skip Prompts: %t\n", cfg.SkipPrompt)
		fmt.Printf("    - Json Logs: %t\n", cfg.UI.JsonLogs)
		fmt.Printf("    - Health Checks: %t\n", cfg.UI.HealthChecks)
		fmt.Printf("    - Preapply Review: %t\n", cfg.UI.PreapplyReview)
	}

	if cfg.Agent != nil {
		fmt.Printf("  Agent Configuration:\n")
		fmt.Printf("    - Max Attempts: %d\n", cfg.Agent.OrchestrationMaxAttempts)
		fmt.Printf("    - Policy Variant: %s\n", cfg.Agent.PolicyVariant)
		fmt.Printf("    - Auto Generate Tests: %t\n", cfg.Agent.AutoGenerateTests)
	}

	if cfg.Security != nil {
		fmt.Printf("  Security Configuration:\n")
		fmt.Printf("    - Security Checks: %t\n", cfg.Security.EnableSecurityChecks)
		fmt.Printf("    - Shell Allowlist: %d\n", len(cfg.Security.ShellAllowlist))
		fmt.Printf("    - Allowed Commands: %d\n", len(cfg.Security.AllowedCommands))
		fmt.Printf("    - Blocked Commands: %d\n", len(cfg.Security.BlockedCommands))
	}

	if cfg.Performance != nil {
		fmt.Printf("  Performance Configuration:\n")
		fmt.Printf("    - Max Concurrent Requests: %d\n", cfg.Performance.MaxConcurrentRequests)
		fmt.Printf("    - Request Delay: %dms\n", cfg.Performance.RequestDelayMs)
	}

	fmt.Printf("  General:\n")
	fmt.Printf("    - Skip Prompt: %t\n", cfg.SkipPrompt)
}