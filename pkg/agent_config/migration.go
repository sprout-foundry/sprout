package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MigrateFromCoder migrates configuration from ~/.coder to ~/.ledit
func MigrateFromCoder() error {
	// Check if old config exists
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	
	oldConfigPath := filepath.Join(homeDir, ".coder", "config.json")
	if _, err := os.Stat(oldConfigPath); os.IsNotExist(err) {
		return nil // No old config to migrate
	}
	
	// Read old config
	oldData, err := os.ReadFile(oldConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read old config: %w", err)
	}
	
	var oldConfig Config
	if err := json.Unmarshal(oldData, &oldConfig); err != nil {
		return fmt.Errorf("failed to parse old config: %w", err)
	}
	
	// Load or create new config
	newConfig, err := Load()
	if err != nil {
		newConfig = NewConfig()
	}
	
	// Migrate data
	if oldConfig.LastUsedProvider != "" {
		newConfig.LastUsedProvider = oldConfig.LastUsedProvider
	}
	
	if len(oldConfig.ProviderModels) > 0 {
		for provider, model := range oldConfig.ProviderModels {
			newConfig.ProviderModels[provider] = model
		}
	}
	
	if len(oldConfig.ProviderPriority) > 0 {
		newConfig.ProviderPriority = oldConfig.ProviderPriority
	}
	
	// Save migrated config
	if err := newConfig.Save(); err != nil {
		return fmt.Errorf("failed to save migrated config: %w", err)
	}
	
	fmt.Println("✅ Configuration migrated from ~/.coder to ~/.ledit")
	
	// Backup and remove old config
	backupPath := oldConfigPath + ".migrated.bak"
	if err := os.Rename(oldConfigPath, backupPath); err != nil {
		fmt.Printf("⚠️  Warning: Could not backup old config file: %v\n", err)
	} else {
		fmt.Printf("📦 Old config backed up to: %s\n", backupPath)
	}
	
	return nil
}

// EnsureLegacyIntegration ensures the new config works with the existing ledit structure
func EnsureLegacyIntegration() error {
	// Load current ledit config if it exists
	leditConfigPath := filepath.Join(os.Getenv("HOME"), ".ledit", "config.json")
	if _, err := os.Stat(leditConfigPath); err == nil {
		// Ledit config exists - we should integrate with it rather than replace it
		return integrateWithLeditConfig()
	}
	
	return nil
}

// integrateWithLeditConfig integrates our agent config with existing ledit config
func integrateWithLeditConfig() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	
	leditConfigPath := filepath.Join(homeDir, ".ledit", "config.json")
	
	// Read existing ledit config
	data, err := os.ReadFile(leditConfigPath)
	if err != nil {
		return err
	}
	
	var leditConfig map[string]interface{}
	if err := json.Unmarshal(data, &leditConfig); err != nil {
		return err
	}
	
	// Load our agent config
	agentConfig, err := Load()
	if err != nil {
		agentConfig = NewConfig()
	}
	
	// Update ledit config with agent provider preferences if they exist
	if agentConfig.LastUsedProvider != "" {
		// Map our provider to ledit's model selection format
		updateLeditModelsFromAgentConfig(leditConfig, agentConfig)
		
		// Save updated ledit config
		updatedData, err := json.MarshalIndent(leditConfig, "", "  ")
		if err != nil {
			return err
		}
		
		if err := os.WriteFile(leditConfigPath, updatedData, 0644); err != nil {
			return err
		}
		
		fmt.Println("✅ Integrated agent configuration with existing ledit config")
	}
	
	return nil
}

// updateLeditModelsFromAgentConfig updates ledit model configuration based on agent config
func updateLeditModelsFromAgentConfig(leditConfig map[string]interface{}, agentConfig *Config) {
	// Map our providers to ledit's model format
	providerModelMap := map[string]string{
		"openai":     "openai:",
		"deepinfra":  "deepinfra:",
		"openrouter": "openrouter:",
		"cerebras":   "cerebras:",
		"groq":       "groq:",
		"deepseek":   "deepseek:",
		"ollama":     "ollama:",
	}
	
	// Update editing_model based on last used provider
	if agentConfig.LastUsedProvider != "" {
		providerName := getProviderConfigName(agentConfig.LastUsedProvider)
		if model, exists := agentConfig.ProviderModels[providerName]; exists {
			if prefix, ok := providerModelMap[providerName]; ok {
				leditConfig["editing_model"] = prefix + model
			}
		}
	}
}