//go:build !js

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

func runMCPTest(serverName string) error {
	reader := bufio.NewReader(os.Stdin)

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
			fmt.Println("No MCP servers configured.")
			fmt.Println("Run 'sprout mcp add' to add a server.")
			return nil
		}

		fmt.Println("Available servers:")
		i := 1
		serverNames := make([]string, 0, len(mcpConfig.Servers))
		for name := range mcpConfig.Servers {
			fmt.Printf("%d. %s\n", i, name)
			serverNames = append(serverNames, name)
			i++
		}

		fmt.Print("Select server to test (1-" + strconv.Itoa(len(serverNames)) + "): ")
		choice, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		choiceNum, err := strconv.Atoi(strings.TrimSpace(choice))
		if err != nil || choiceNum < 1 || choiceNum > len(serverNames) {
			return fmt.Errorf("invalid choice: %s", choice)
		}

		serverName = serverNames[choiceNum-1]
	}

	// Check if server exists
	serverConfig, exists := mcpConfig.Servers[serverName]
	if !exists {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	fmt.Printf("[test] Testing MCP Server: %s\n", serverName)
	fmt.Println("========================")
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

	fmt.Println("[...] Starting server...")
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	defer func() {
		fmt.Println("⏹ Stopping server...")
		server.Stop(context.Background())
	}()

	fmt.Println("✓ Server started successfully!")

	fmt.Println("[~] Initializing server...")
	if err := server.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}
	fmt.Println("✓ Server initialized successfully!")

	console.GlyphAction.Print("Listing available tools...")
	tools, err := server.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	if len(tools) == 0 {
		fmt.Println("⚠ No tools available from this server.")
		return nil
	}

	fmt.Printf("✓ Found %d tools:\n", len(tools))
	fmt.Println()

	for i, tool := range tools {
		fmt.Printf("%d. %s\n", i+1, tool.Name)
		if tool.Description != "" {
			fmt.Printf("   Description: %s\n", tool.Description)
		}
		fmt.Println()
	}

	fmt.Printf("\n[done] Test completed successfully! Server '%s' is working properly.\n", serverName)

	return nil
}
