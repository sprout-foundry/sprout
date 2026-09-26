//go:build !js

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

func runMCPAdd() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("MCP Server Setup")
	fmt.Println("==================")
	fmt.Println()

	// Load existing config
	_, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Get templates from registry
	registry := mcp.NewMCPServerRegistry()
	templates := registry.ListTemplates()

	if len(templates) == 0 {
		return errors.New("no templates available. Add templates to ~/.config/sprout/mcp_templates.json")
	}

	// Display templates (filter out generic templates for main menu)
	var visibleTemplates []mcp.MCPServerTemplate
	for _, t := range templates {
		if t.ID == "http-generic" || t.ID == "stdio-generic" {
			continue
		}
		visibleTemplates = append(visibleTemplates, t)
	}

	items := make([]console.SelectItem, 0, len(visibleTemplates)+1)
	for _, t := range visibleTemplates {
		items = append(items, console.SelectItem{
			Label:  t.Name,
			Detail: t.Description,
			Value:  t.ID,
		})
	}
	items = append(items, console.SelectItem{
		Label:  "Custom MCP Server (stdio or http)",
		Detail: "configure manually",
		Value:  "__custom__",
	})

	sl := console.NewSelectList(console.SelectListOptions{
		Title:      "Pick a server type",
		Items:      items,
		Searchable: true,
		PageSize:   10,
	})

	ctx := context.Background()
	value, ok, err := sl.Run(ctx)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println()
		console.GlyphInfo.Print("Setup cancelled.")
		return nil
	}

	// Handle custom server option
	if value == "__custom__" {
		return setupCustomMCPServer(&mcpConfig, reader, registry)
	}

	// Find the matching template by ID (picker returns the template's ID).
	var selectedTemplate mcp.MCPServerTemplate
	for _, t := range visibleTemplates {
		if t.ID == value {
			selectedTemplate = t
			break
		}
	}
	if selectedTemplate.ID == "" {
		return fmt.Errorf("unknown template: %s", value)
	}

	// Dispatch to a rich guided setup flow when one is available for the
	// selected template. These guided flows (Git, GitHub, Playwright, Chrome
	// DevTools) collect richer input than the generic template path and offer
	// installation-method pickers, so they take precedence when matched.
	if guided, ok := guidedSetupFor(selectedTemplate.ID); ok {
		return guided(&mcpConfig, reader)
	}

	// Prompt for server name (with default from template)
	serverName := selectedTemplate.ID
	if selectedTemplate.Type == "stdio" || selectedTemplate.Type == "http" {
		fmt.Printf("Enter server name [%s]: ", selectedTemplate.ID)
		nameInput, _ := reader.ReadString('\n')
		nameInput = strings.TrimSpace(nameInput)
		if nameInput != "" {
			serverName = nameInput
		}
	}

	// Check if server already exists
	if _, exists := mcpConfig.Servers[serverName]; exists {
		fmt.Printf("Server '%s' is already configured. Reconfigure? (y/N): ", serverName)
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Collect environment variables if required
	envValues := make(map[string]string)
	for _, envVar := range selectedTemplate.EnvVars {
		prompt := fmt.Sprintf("Enter %s", envVar.Name)
		if envVar.Description != "" {
			prompt += fmt.Sprintf(" (%s)", envVar.Description)
		}
		if envVar.Default != "" {
			prompt += fmt.Sprintf(" [%s]", envVar.Default)
		}
		prompt += ": "

		fmt.Print(prompt)
		value, _ := reader.ReadString('\n')
		value = strings.TrimSpace(value)
		if value == "" && envVar.Default != "" {
			value = envVar.Default
		}
		if value != "" || envVar.Required {
			envValues[envVar.Name] = value
		}
	}

	// For HTTP servers, prompt for URL if not set in template
	customURL := ""
	if selectedTemplate.Type == "http" && selectedTemplate.URL == "" {
		fmt.Print("Enter server URL: ")
		customURL, _ = reader.ReadString('\n')
		customURL = strings.TrimSpace(customURL)
	}

	// For stdio servers, allow custom command/args
	customCommand := ""
	customArgs := []string{}
	if selectedTemplate.ID == "git-uvx" || selectedTemplate.ID == "git" {
		// Special handling for git - ask for repo path
		fmt.Print("Enter repository path (optional, leave empty to use current directory): ")
		repoPath, _ := reader.ReadString('\n')
		repoPath = strings.TrimSpace(repoPath)
		if repoPath != "" {
			customArgs = []string{"mcp-server-git", "--repository", repoPath}
		}
	}

	// Create server config from template
	serverConfig := selectedTemplate.CreateServerConfig(serverName, envValues, customURL, customCommand, customArgs)

	// Add server to config
	mcpConfig.Servers[serverName] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	if err := mcp.SaveMCPConfig(&mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	console.GlyphSuccess.Fprintf(os.Stdout, "%s configured successfully!", serverConfig.Name)
	fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	fmt.Println()
	fmt.Printf("To test the configuration, run: sprout mcp test %s\n", serverName)
	fmt.Println()

	if selectedTemplate.Docs != "" {
		fmt.Printf("%sDocumentation: %s\n", console.GlyphInfo.Prefix(), selectedTemplate.Docs)
	}

	return nil
}

// mcpSetupFunc is the signature shared by the guided per-server setup flows.
type mcpSetupFunc func(mcpConfig *mcp.MCPConfig, reader *bufio.Reader) error

// guidedSetupFor returns the rich guided setup function for a template ID, if
// one exists. Templates with a guided flow (Git, GitHub, Playwright, Chrome
// DevTools) install-method pickers and richer prompts than the generic
// template-driven path, so runMCPAdd dispatches to them when matched.
//
// Mapping multiple template IDs to the same flow (e.g. both "git" and the
// registry's "git-uvx" entry route to setupGitMCPServer) keeps the picker
// resilient to renamed/aliased template IDs.
func guidedSetupFor(templateID string) (mcpSetupFunc, bool) {
	guidedSetups := map[string]mcpSetupFunc{
		"git":             setupGitMCPServer,
		"git-uvx":         setupGitMCPServer,
		"playwright":      setupPlaywrightMCPServer,
		"chrome-devtools": setupChromeDevToolsMCPServer,
	}
	fn, ok := guidedSetups[templateID]
	return fn, ok
}
