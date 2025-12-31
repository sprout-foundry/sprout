package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/spf13/cobra"
)

var diagCmd = &cobra.Command{
	Use:   "diag",
	Short: "Show diagnostic information about configuration",
	Long:  `Display which config files exist and where custom providers are stored.`,
	Run: func(cmd *cobra.Command, args []string) {
		runDiag()
	},
}

func runDiag() {
	fmt.Println("=== Ledit Configuration Diagnostics ===")
	fmt.Println()

	// Check global config
	homeDir, _ := os.UserHomeDir()
	globalConfigPath := filepath.Join(homeDir, configuration.ConfigDirName, configuration.ConfigFileName)
	projectConfigPath := filepath.Join(".", configuration.ConfigDirName, configuration.ConfigFileName)

	fmt.Printf("Global config: %s\n", globalConfigPath)
	if info, err := os.Stat(globalConfigPath); err == nil {
		fmt.Printf("  ✅ EXISTS (modified: %s)\n", info.ModTime().Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("  ❌ Does not exist\n")
	}
	fmt.Println()

	fmt.Printf("Project-local config: %s\n", projectConfigPath)
	if info, err := os.Stat(projectConfigPath); err == nil {
		fmt.Printf("  ✅ EXISTS (modified: %s)\n", info.ModTime().Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("  ❌ Does not exist\n")
	}
	fmt.Println()

	// Check what the code will actually load
	fmt.Printf("Loaded config path: %s\n", globalConfigPath)
	fmt.Println("(Note: ledit currently ONLY uses global config, not project-local)")
	fmt.Println()

	// Load and show custom providers
	config, err := configuration.Load()
	if err != nil {
		fmt.Printf("❌ Error loading config: %v\n", err)
		return
	}

	if config.CustomProviders == nil || len(config.CustomProviders) == 0 {
		fmt.Println("⚠️  No custom providers configured")
	} else {
		fmt.Printf("Custom providers found: %d\n", len(config.CustomProviders))
		for name, provider := range config.CustomProviders {
			fmt.Printf("  • %s\n", name)
			fmt.Printf("    Endpoint: %s\n", provider.Endpoint)
			fmt.Printf("    Model: %s\n", provider.ModelName)
			fmt.Printf("    Context: %d tokens\n", provider.ContextSize)
		}
	}
	fmt.Println()

	// Show where provider is registered
	if config.ProviderModels != nil {
		for provider, model := range config.ProviderModels {
			if _, isCustom := config.CustomProviders[provider]; isCustom {
				fmt.Printf("✅ Custom provider '%s' is in provider_models (model: %s)\n", provider, model)
			}
		}
	}
	fmt.Println()

	if config.ProviderPriority != nil {
		customInPriority := 0
		for _, provider := range config.ProviderPriority {
			if _, isCustom := config.CustomProviders[provider]; isCustom {
				customInPriority++
				fmt.Printf("✅ Custom provider '%s' is in provider_priority\n", provider)
			}
		}
		if customInPriority == 0 && len(config.CustomProviders) > 0 {
			fmt.Println("⚠️  Custom providers exist but are NOT in provider_priority")
		}
	}
}

func init() {
	rootCmd.AddCommand(diagCmd)
}
