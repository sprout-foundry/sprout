//go:build !js

package cmd

import (
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP (Model Context Protocol) servers",
	Long: `Manage MCP servers that extend sprout with external tools and services.
Use subcommands to add, remove, or list configured MCP servers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var mcpAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new MCP server interactively",
	Long: `Interactively add a new MCP server configuration.
This will guide you through setting up popular MCP servers or custom configurations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMCPAdd()
	},
}

var mcpRemoveCmd = &cobra.Command{
	Use:   "remove [server-name]",
	Short: "Remove an MCP server",
	Long:  `Remove an MCP server from the configuration.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var serverName string
		if len(args) > 0 {
			serverName = args[0]
		}
		return runMCPRemove(serverName)
	},
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured MCP servers",
	Long:  `Display all configured MCP servers and their status.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMCPList()
	},
}

var mcpTestCmd = &cobra.Command{
	Use:   "test [server-name]",
	Short: "Test MCP server connection",
	Long:  `Test connection to an MCP server and list its available tools.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var serverName string
		if len(args) > 0 {
			serverName = args[0]
		}
		return runMCPTest(serverName)
	},
}

func init() {
	mcpCmd.AddCommand(mcpAddCmd)
	mcpCmd.AddCommand(mcpRemoveCmd)
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpTestCmd)
}
