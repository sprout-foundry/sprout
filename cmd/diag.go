//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/pythonruntime"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

var diagCmd = &cobra.Command{
	Use:   "diag",
	Short: "Show diagnostic information about configuration",
	Long:  `Display which config files exist and where custom providers are stored.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		runDiag()
		return nil
	},
}

func runDiag() {
	fmt.Println("=== Sprout Configuration Diagnostics ===")
	fmt.Println()

	// Check global config
	homeDir, _ := os.UserHomeDir()
	globalConfigPath := filepath.Join(homeDir, configuration.ConfigDirName, configuration.ConfigFileName)
	projectConfigPath := filepath.Join(".", configuration.ConfigDirName, configuration.ConfigFileName)

	fmt.Printf("Global config: %s\n", globalConfigPath)
	if info, err := os.Stat(globalConfigPath); err == nil {
		fmt.Printf("  %sEXISTS (modified: %s)\n", console.GlyphSuccess.Prefix(), info.ModTime().Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("  %sDoes not exist\n", console.GlyphError.Prefix())
	}
	fmt.Println()

	fmt.Printf("Project-local config: %s\n", projectConfigPath)
	if info, err := os.Stat(projectConfigPath); err == nil {
		fmt.Printf("  %sEXISTS (modified: %s)\n", console.GlyphSuccess.Prefix(), info.ModTime().Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("  %sDoes not exist\n", console.GlyphError.Prefix())
	}
	fmt.Println()

	// Check what the code will actually load
	fmt.Printf("Loaded config path: %s\n", globalConfigPath)
	fmt.Println("(Note: sprout currently ONLY uses global config, not project-local)")
	fmt.Println()

	providersDir, _ := configuration.GetProvidersDir()
	fmt.Printf("Custom provider directory: %s\n", providersDir)
	fmt.Println()

	// Load and show custom providers
	config, err := configuration.Load()
	if err != nil {
		console.GlyphError.Fprintf(os.Stdout, "Error loading config: %v", err)
		return
	}

	if config.CustomProviders == nil || len(config.CustomProviders) == 0 {
		console.GlyphWarning.Fprintln(os.Stdout, "No custom providers configured")
	} else {
		fmt.Printf("Custom providers found: %d\n", len(config.CustomProviders))
		for name, provider := range config.CustomProviders {
			fmt.Printf("  • %s\n", name)
			fmt.Printf("    Endpoint: %s\n", secretdetect.RedactOpaque(provider.Endpoint))
			fmt.Printf("    Model: %s\n", provider.ModelName)
			fmt.Printf("    Context: %d tokens\n", provider.ContextSize)
		}
	}
	fmt.Println()

	// MCP diagnostics
	fmt.Println("MCP Configuration")
	fmt.Println("=================")
	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		console.GlyphError.Fprintf(os.Stdout, "  Error loading MCP config: %v", err)
	} else {
		fmt.Printf("  Enabled: %t\n", mcpConfig.Enabled)
		fmt.Printf("  Auto-start: %t\n", mcpConfig.AutoStart)
		fmt.Printf("  Auto-discover: %t\n", mcpConfig.AutoDiscover)
		fmt.Printf("  Total servers: %d\n", len(mcpConfig.Servers))
		fmt.Println()

		if len(mcpConfig.Servers) == 0 {
			fmt.Println("  ⓘ No MCP servers configured")
		} else {
			fmt.Println("  Configured Servers:")
			redactedConfig := mcp.RedactMCPConfig(mcpConfig)
			for name, server := range redactedConfig.Servers {
				fmt.Printf("    • %s\n", name)
				if server.Type == "http" {
					fmt.Printf("      Type: HTTP Remote Server\n")
					fmt.Printf("      URL: %s\n", secretdetect.RedactOpaque(server.URL))
				} else {
					fmt.Printf("      Command: %s %v\n", server.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", server.Args)))
				}
				fmt.Printf("      Auto-start: %t\n", server.AutoStart)
				fmt.Printf("      Max restarts: %d\n", server.MaxRestarts)
				fmt.Printf("      Timeout: %v\n", server.Timeout)

				if server.WorkingDir != "" {
					fmt.Printf("      Working dir: %s\n", server.WorkingDir)
				}

				// Show redacted env vars
				if len(server.Env) > 0 {
					redactedEnv := credentials.RedactEnvMap(server.Env)
					fmt.Printf("      Env vars (%d): ", len(redactedEnv))
					envEntries := make([]string, 0, len(redactedEnv))
					for k, v := range redactedEnv {
						envEntries = append(envEntries, fmt.Sprintf("%s=%s", k, v))
					}
					fmt.Printf("%s\n", strings.Join(envEntries, ", "))
				}

				// Show credentials (placeholder references are safe; actual secrets are masked)
				if len(server.Credentials) > 0 {
					redactedCreds := credentials.RedactMap(server.Credentials)
					credEntries := make([]string, 0, len(redactedCreds))
					for k, v := range redactedCreds {
						credEntries = append(credEntries, fmt.Sprintf("%s=%s", k, v))
					}
					fmt.Printf("      Credentials (%d): %s\n", len(redactedCreds), strings.Join(credEntries, ", "))
				}

				fmt.Println()
			}
		}
	}
	fmt.Println()

	// Check python availability for runtime tooling
	fmt.Println("Python runtime:")
	if interp, err := pythonruntime.FindPython3Interpreter(); err != nil {
		fmt.Printf("  %sPython 3 runtime not found: %v\n", console.GlyphError.Prefix(), err)
	} else {
		fmt.Printf("  %sPython 3 runtime: %s (%s)\n", console.GlyphSuccess.Prefix(), interp.Path, interp.Version)
	}
	fmt.Println()

	fmt.Println("PDF Python runtime (requires 3.10+):")
	if err := tools.CheckPDFPython3Available(); err != nil {
		fmt.Printf("  %sPDF runtime precheck failed: %v\n", console.GlyphError.Prefix(), err)
	} else if interp, err := pythonruntime.FindPython3InterpreterAtLeast(10); err == nil {
		fmt.Printf("  %sPDF runtime: %s (%s)\n", console.GlyphSuccess.Prefix(), interp.Path, interp.Version)
	} else {
		fmt.Printf("  %sPDF runtime available\n", console.GlyphSuccess.Prefix())
	}
	fmt.Println()

	// Show where provider is registered
	if config.ProviderModels != nil {
		for provider, model := range config.ProviderModels {
			if _, isCustom := config.CustomProviders[provider]; isCustom {
				console.GlyphSuccess.Fprintf(os.Stdout, "Custom provider '%s' is in provider_models (model: %s)", provider, model)
			}
		}
	}
	fmt.Println()

	if config.ProviderPriority != nil {
		customInPriority := 0
		for _, provider := range config.ProviderPriority {
			if _, isCustom := config.CustomProviders[provider]; isCustom {
				customInPriority++
				console.GlyphSuccess.Fprintf(os.Stdout, "Custom provider '%s' is in provider_priority", provider)
			}
		}
		if customInPriority == 0 && len(config.CustomProviders) > 0 {
			console.GlyphWarning.Fprintln(os.Stdout, "Custom providers exist but are NOT in provider_priority")
		}
	}
}

func init() {
	rootCmd.AddCommand(diagCmd)
}
