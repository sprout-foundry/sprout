package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/alantheprice/ledit/pkg/mcp"
	"github.com/alantheprice/ledit/pkg/pythonruntime"
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
		fmt.Printf("  [OK] EXISTS (modified: %s)\n", info.ModTime().Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("  [FAIL] Does not exist\n")
	}
	fmt.Println()

	fmt.Printf("Project-local config: %s\n", projectConfigPath)
	if info, err := os.Stat(projectConfigPath); err == nil {
		fmt.Printf("  [OK] EXISTS (modified: %s)\n", info.ModTime().Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("  [FAIL] Does not exist\n")
	}
	fmt.Println()

	// Check what the code will actually load
	fmt.Printf("Loaded config path: %s\n", globalConfigPath)
	fmt.Println("(Note: ledit currently ONLY uses global config, not project-local)")
	fmt.Println()

	providersDir, _ := configuration.GetProvidersDir()
	fmt.Printf("Custom provider directory: %s\n", providersDir)
	fmt.Println()

	// Load and show custom providers
	config, err := configuration.Load()
	if err != nil {
		fmt.Printf("[FAIL] Error loading config: %v\n", err)
		return
	}

	if config.CustomProviders == nil || len(config.CustomProviders) == 0 {
		fmt.Println("[WARN] No custom providers configured")
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

	// MCP diagnostics
	fmt.Println("MCP Configuration")
	fmt.Println("=================")
	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		fmt.Printf("  [FAIL] Error loading MCP config: %v\n", err)
	} else {
		fmt.Printf("  Enabled: %t\n", mcpConfig.Enabled)
		fmt.Printf("  Auto-start: %t\n", mcpConfig.AutoStart)
		fmt.Printf("  Auto-discover: %t\n", mcpConfig.AutoDiscover)
		fmt.Printf("  Total servers: %d\n", len(mcpConfig.Servers))
		fmt.Println()

		if len(mcpConfig.Servers) == 0 {
			fmt.Println("  [INFO] No MCP servers configured")
		} else {
			fmt.Println("  Configured Servers:")
			for name, server := range mcpConfig.Servers {
				fmt.Printf("    • %s\n", name)
				if server.Type == "http" {
					fmt.Printf("      Type: HTTP Remote Server\n")
					fmt.Printf("      URL: %s\n", credentials.RedactLogLine(server.URL))
				} else {
					fmt.Printf("      Command: %s %v\n", server.Command, server.Args)
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
		fmt.Printf("  [FAIL] Python 3 runtime not found: %v\n", err)
	} else {
		fmt.Printf("  [OK] Python 3 runtime: %s (%s)\n", interp.Path, interp.Version)
	}
	fmt.Println()

	fmt.Println("PDF Python runtime (requires 3.10+):")
	if err := tools.CheckPDFPython3Available(); err != nil {
		fmt.Printf("  [FAIL] PDF runtime precheck failed: %v\n", err)
	} else if interp, err := pythonruntime.FindPython3InterpreterAtLeast(10); err == nil {
		fmt.Printf("  [OK] PDF runtime: %s (%s)\n", interp.Path, interp.Version)
	} else {
		fmt.Println("  [OK] PDF runtime available")
	}
	fmt.Println()

	// Show where provider is registered
	if config.ProviderModels != nil {
		for provider, model := range config.ProviderModels {
			if _, isCustom := config.CustomProviders[provider]; isCustom {
				fmt.Printf("[OK] Custom provider '%s' is in provider_models (model: %s)\n", provider, model)
			}
		}
	}
	fmt.Println()

	if config.ProviderPriority != nil {
		customInPriority := 0
		for _, provider := range config.ProviderPriority {
			if _, isCustom := config.CustomProviders[provider]; isCustom {
				customInPriority++
				fmt.Printf("[OK] Custom provider '%s' is in provider_priority\n", provider)
			}
		}
		if customInPriority == 0 && len(config.CustomProviders) > 0 {
			fmt.Println("[WARN] Custom providers exist but are NOT in provider_priority")
		}
	}
}

func init() {
	rootCmd.AddCommand(diagCmd)
}
