//go:build !js

package cmd

// mcp_add_browsers.go — the browser-based MCP server setup flows for
// `sprout mcp add`: setupPlaywrightMCPServer and
// setupChromeDevToolsMCPServer (guided install prompts + config write).
// Shared prompt helper: promptInstallMethod (mcp_add_servers.go). Split
// out of mcp_add.go.
import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

func setupPlaywrightMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader) error {
	fmt.Println()
	console.GlyphInfo.Print("Playwright MCP Server Setup")
	fmt.Println("=============================")
	fmt.Println()

	// Check if Playwright server already exists
	if _, exists := mcpConfig.Servers["playwright"]; exists {
		fmt.Print("Playwright MCP server is already configured. Reconfigure? (y/N): ")
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Installation method picker
	installItems := []console.SelectItem{
		{Label: "Official Playwright MCP Server (recommended)", Value: "1"},
		{Label: "Automata Labs Playwright MCP Server", Value: "2"},
		{Label: "Execute Automation Playwright MCP Server", Value: "3"},
	}
	installChoice, ok, err := promptInstallMethod(reader, installItems)
	if err != nil {
		return fmt.Errorf("failed to read installation method: %w", err)
	}
	if !ok {
		fmt.Println()
		console.GlyphInfo.Print("Setup cancelled.")
		return nil
	}

	var serverConfig mcp.MCPServerConfig

	switch installChoice {
	case "1":
		// Official Playwright MCP Server
		serverConfig = mcp.MCPServerConfig{
			Name:        "playwright",
			Command:     "npx",
			Args:        []string{"-y", "@playwright/mcp"},
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     60 * time.Second, // Longer timeout for browser operations
		}
	case "2":
		// Automata Labs Playwright MCP Server
		serverConfig = mcp.MCPServerConfig{
			Name:        "playwright",
			Command:     "npx",
			Args:        []string{"-y", "@automatalabs/mcp-server-playwright"},
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     60 * time.Second,
		}
	case "3":
		// Execute Automation Playwright MCP Server
		serverConfig = mcp.MCPServerConfig{
			Name:        "playwright",
			Command:     "npx",
			Args:        []string{"-y", "@executeautomation/playwright-mcp-server"},
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     60 * time.Second,
		}
	default:
		return fmt.Errorf("invalid choice: %s", installChoice)
	}

	// Add server to config
	mcpConfig.Servers["playwright"] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	if err := mcp.SaveMCPConfig(mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	console.GlyphSuccess.Fprintln(os.Stdout, "Playwright MCP Server configured successfully!")
	fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	fmt.Println()
	fmt.Println("To test the configuration, run: sprout mcp test playwright")
	fmt.Println()
	console.GlyphInfo.Print("Installation (if not already installed):")
	fmt.Println("npx will install the package automatically")
	fmt.Println()
	console.GlyphInfo.Print("Features available:")
	fmt.Println("• Browser automation (Chromium, Firefox, WebKit)")
	fmt.Println("• Web scraping and data extraction")
	fmt.Println("• UI testing and validation")
	fmt.Println("• Screenshot capture and visual testing")
	fmt.Println("• Form filling and navigation automation")

	return nil
}

func setupChromeDevToolsMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Println("ⓘ Chrome DevTools MCP Server Setup")
	fmt.Println("====================================")
	fmt.Println()

	// Check if Chrome DevTools server already exists
	if _, exists := mcpConfig.Servers["chrome-devtools"]; exists {
		fmt.Print("Chrome DevTools MCP server is already configured. Reconfigure? (y/N): ")
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Chrome DevTools MCP Server
	serverConfig := mcp.MCPServerConfig{
		Name:        "chrome-devtools",
		Command:     "npx",
		Args:        []string{"-y", "chrome-devtools-mcp@latest", "--isolated"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     60 * time.Second, // Longer timeout for browser operations
	}

	// Optional configuration picker
	configItems := []console.SelectItem{
		{Label: "Default settings (recommended)", Value: "1"},
		{Label: "Headless mode (no visible browser window)", Value: "2"},
		{Label: "Custom Chrome channel (stable/beta/dev/canary)", Value: "3"},
	}
	configChoice, ok, err := promptInstallMethod(reader, configItems)
	if err != nil {
		return fmt.Errorf("failed to read configuration choice: %w", err)
	}
	if !ok {
		fmt.Println()
		console.GlyphInfo.Print("Setup cancelled.")
		return nil
	}

	switch configChoice {
	case "2":
		// Headless mode
		serverConfig.Args = append(serverConfig.Args, "--headless=true")
	case "3":
		fmt.Print("Enter Chrome channel (stable/beta/dev/canary) [stable]: ")
		channelInput, _ := reader.ReadString('\n')
		channel := strings.TrimSpace(channelInput)
		if channel == "" {
			channel = "stable"
		}
		serverConfig.Args = append(serverConfig.Args, "--channel="+channel)
	}

	// Add server to config
	mcpConfig.Servers["chrome-devtools"] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	if err := mcp.SaveMCPConfig(mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	console.GlyphSuccess.Fprintln(os.Stdout, "Chrome DevTools MCP Server configured successfully!")
	fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	fmt.Println()
	fmt.Println("To test the configuration, run: sprout mcp test chrome-devtools")
	fmt.Println()
	console.GlyphInfo.Print("Installation (if not already installed):")
	fmt.Println("npx will install the package automatically")
	fmt.Println()
	fmt.Println("ⓘ Features available:")
	fmt.Println("• Browser automation (click, fill forms, navigation)")
	fmt.Println("• Performance analysis and tracing")
	fmt.Println("• Network request inspection")
	fmt.Println("• Console message capture")
	fmt.Println("• Screenshot and snapshot capture")
	fmt.Println("• DOM inspection and scripting")
	fmt.Println()
	fmt.Println("[read] Documentation: https://github.com/ChromeDevTools/chrome-devtools-mcp")

	return nil
}
