//go:build !js

package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

var mcpEditCmd = &cobra.Command{
	Use:   "edit [server-name]",
	Short: "Edit a configured MCP server",
	Long: `Edit a configured MCP server without removing and re-adding it.

Currently this changes one setting: whether the server's tool output is framed
as untrusted data. Omit --trust-untrusted-content to be asked; pass it to set
the value directly (e.g. in a script).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := ""
		if len(args) > 0 {
			serverName = args[0]
		}
		// The setting is a bool, so an explicit --trust-untrusted-content=false
		// must be distinguishable from not passing the flag at all, or the
		// command could never turn framing off without prompting.
		trust, err := cmd.Flags().GetBool("trust-untrusted-content")
		if err != nil {
			return err
		}
		return runMCPEdit(serverName, trust, cmd.Flags().Changed("trust-untrusted-content"))
	},
}

func init() {
	mcpEditCmd.Flags().Bool("trust-untrusted-content", true,
		"Frame this server's output as untrusted data (omit to be asked)")
}

func runMCPEdit(serverName string, trust bool, trustSet bool) error {
	if _, err := configuration.LoadOrInitConfig(false); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}
	if len(mcpConfig.Servers) == 0 {
		fmt.Println("No MCP servers configured.")
		return nil
	}

	if serverName == "" {
		serverName, err = pickServerForEdit(mcpConfig)
		if err != nil {
			return err
		}
		if serverName == "" {
			return nil // cancelled
		}
	}

	if !trustSet {
		trust, err = promptTrustUntrustedContent(serverName, bufio.NewReader(os.Stdin))
		if err != nil {
			return err
		}
	}

	before, err := applyMCPTrustEdit(&mcpConfig, serverName, trust)
	if err != nil {
		return err
	}

	if err := mcp.SaveMCPConfig(&mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	if before == trust {
		console.GlyphSuccess.Fprintf(os.Stdout, "Server '%s': trust_untrusted_content already %t (saved).\n", serverName, trust)
		return nil
	}
	console.GlyphSuccess.Fprintf(os.Stdout, "Server '%s': trust_untrusted_content %t -> %t.\n", serverName, before, trust)
	return nil
}

// applyMCPTrustEdit records the framing choice on a loaded config and reports
// the effective value before the edit. It is separate from runMCPEdit so the
// mutation can be tested without touching the user's real config directory.
func applyMCPTrustEdit(mcpConfig *mcp.MCPConfig, serverName string, trust bool) (bool, error) {
	server, ok := mcpConfig.Servers[serverName]
	if !ok {
		return false, fmt.Errorf("server '%s' not found", serverName)
	}
	// OrDefault, not the raw field: a server configured before the setting
	// existed has no recorded value, and reporting "false -> false" for it
	// would claim the server was already opted out when it was actually
	// framed by default.
	before := server.TrustUntrustedContentOrDefault()
	server.SetTrustUntrustedContent(trust)
	mcpConfig.Servers[serverName] = server
	return before, nil
}

// pickServerForEdit offers the configured servers in a stable order. Returns
// an empty name when the user cancels.
func pickServerForEdit(mcpConfig mcp.MCPConfig) (string, error) {
	names := make([]string, 0, len(mcpConfig.Servers))
	for name := range mcpConfig.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]console.SelectItem, 0, len(names))
	for _, name := range names {
		server := mcpConfig.Servers[name]
		items = append(items, console.SelectItem{
			Label:  name,
			Detail: server.Type,
			Value:  name,
		})
	}

	sl := console.NewSelectList(console.SelectListOptions{
		Title:      "Pick a server to edit",
		Items:      items,
		Searchable: true,
		PageSize:   10,
	})

	value, ok, err := sl.Run(context.Background())
	if err != nil {
		return "", err
	}
	if !ok {
		fmt.Println()
		console.GlyphInfo.Print("Edit cancelled.")
		return "", nil
	}
	return value, nil
}
