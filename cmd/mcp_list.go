//go:build !js

package cmd

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

func runMCPList() error {
	// Load existing config
	_, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Redact MCP config to remove sensitive data before displaying
	redactedConfig := mcp.RedactMCPConfig(mcpConfig)

	fmt.Println("MCP Configuration")
	fmt.Println("==================")
	fmt.Printf("Enabled: %t\n", redactedConfig.Enabled)
	fmt.Printf("Auto-start: %t\n", redactedConfig.AutoStart)
	fmt.Printf("Auto-discover: %t\n", redactedConfig.AutoDiscover)
	fmt.Printf("Default timeout: %v\n", redactedConfig.Timeout)
	fmt.Printf("Total servers: %d\n", len(redactedConfig.Servers))
	fmt.Println()

	if len(redactedConfig.Servers) == 0 {
		fmt.Println("No MCP servers configured.")
		fmt.Println("Run 'sprout mcp add' to add a server.")
		return nil
	}

	fmt.Println("Configured Servers:")
	fmt.Println("-------------------")

	for name, server := range redactedConfig.Servers {
		fmt.Printf("%s%s\n", console.GlyphSuccess.Prefix(), name)
		if server.Type == "http" {
			fmt.Printf("   Type: HTTP Remote Server\n")
			fmt.Printf("   URL: %s\n", secretdetect.RedactOpaque(server.URL))
		} else {
			fmt.Printf("   Command: %s %v\n", server.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", server.Args)))
		}
		fmt.Printf("   Auto-start: %t\n", server.AutoStart)
		fmt.Printf("   Max restarts: %d\n", server.MaxRestarts)
		fmt.Printf("   Timeout: %v\n", server.Timeout)

		if server.WorkingDir != "" {
			fmt.Printf("   Working dir: %s\n", server.WorkingDir)
		}

		if len(server.Env) > 0 {
			fmt.Printf("   Environment vars: ")
			envEntries := make([]string, 0, len(server.Env))
			for key, value := range server.Env {
				envEntries = append(envEntries, key+"="+value)
			}
			fmt.Printf("%s\n", strings.Join(envEntries, ", "))
		}

		// Show credentials if present (placeholder references are safe; actual secrets are masked)
		if len(server.Credentials) > 0 {
			fmt.Printf("   Credentials: ")
			credEntries := make([]string, 0, len(server.Credentials))
			for key, value := range server.Credentials {
				credEntries = append(credEntries, key+"="+value)
			}
			fmt.Printf("%s\n", strings.Join(credEntries, ", "))
		}

		fmt.Println()
	}

	fmt.Println("Commands:")
	fmt.Println("  sprout mcp test [server] - Test server connection")
	fmt.Println("  sprout mcp add          - Add new server")
	fmt.Println("  sprout mcp remove       - Remove server")

	return nil
}
