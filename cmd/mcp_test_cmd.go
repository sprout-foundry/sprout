//go:build !js

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

func runMCPTest(serverName string) error {
	// Load existing config
	_, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// If no server name provided, list available servers
	if serverName == "" {
		if len(mcpConfig.Servers) == 0 {
			console.GlyphInfo.Print("No MCP servers configured.")
			console.Hintln(os.Stderr, "Run 'sprout mcp add' to add a server.")
			return nil
		}

		// Stable, alphabetically-ordered list so the picker is
		// deterministic across runs (map iteration order is not).
		serverNames := make([]string, 0, len(mcpConfig.Servers))
		for name := range mcpConfig.Servers {
			serverNames = append(serverNames, name)
		}
		sort.Strings(serverNames)

		items := make([]console.SelectItem, 0, len(serverNames))
		for _, name := range serverNames {
			srv := mcpConfig.Servers[name]
			items = append(items, console.SelectItem{
				Label:  name,
				Detail: srv.Type,
				Value:  name,
			})
		}

		sl := console.NewSelectList(console.SelectListOptions{
			Title:      "Pick a server to test",
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
			console.GlyphInfo.Print("Test cancelled.")
			return nil
		}
		serverName = value
	}

	// Check if server exists
	serverConfig, exists := mcpConfig.Servers[serverName]
	if !exists {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	console.Heading(os.Stdout, "Testing MCP server: "+serverName)
	fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	fmt.Println()

	// Create manager and client
	manager := mcp.NewMCPManager(nil)
	if err := manager.AddServer(serverConfig); err != nil {
		return fmt.Errorf("failed to add server to manager: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), serverConfig.Timeout+10*time.Second)
	defer cancel()

	server, exists := manager.GetServer(serverName)
	if !exists {
		return errors.New("failed to get server from manager")
	}

	console.GlyphAction.Print("Starting server…")
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	defer func() {
		console.GlyphStopped.Print("Stopping server…")
		server.Stop(context.Background())
	}()

	console.GlyphSuccess.Fprintln(os.Stdout, "Server started successfully!")

	console.GlyphAction.Print("Initializing server…")
	if err := server.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}
	console.GlyphSuccess.Fprintln(os.Stdout, "Server initialized successfully!")

	console.GlyphAction.Print("Listing available tools...")
	tools, err := server.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	if len(tools) == 0 {
		console.GlyphWarning.Fprintln(os.Stdout, "No tools available from this server.")
		return nil
	}

	console.GlyphSuccess.Fprintf(os.Stdout, "Found %d tools:", len(tools))
	fmt.Println()

	for i, tool := range tools {
		fmt.Printf("%d. %s\n", i+1, tool.Name)
		if tool.Description != "" {
			fmt.Printf("   Description: %s\n", tool.Description)
		}
		fmt.Println()
	}

	console.GlyphSuccess.Printf("\nTest completed successfully! Server '%s' is working properly.\n", serverName)

	return nil
}
