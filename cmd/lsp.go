//go:build !js

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/lsp/proxy"
)

var lspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Manage Language Server Protocol (LSP) servers",
	Long: `Manage language server configurations that provide IDE-like features
such as code completion, diagnostics, and go-to-definition.
Use subcommands to list, install, or check the status of language servers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var lspListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured language servers and their install status",
	Long:  `Display all supported language servers and whether their binary is installed on your system.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLSPList()
	},
}

var lspInstallCmd = &cobra.Command{
	Use:   "install [language]",
	Short: "Show installation instructions for a language server",
	Long:  `Look up and display the installation instructions for the language server that handles the given language.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLSPInstall(args[0])
	},
}

var lspStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show detailed status of language servers",
	Long: `Show detailed status information for each language server including
binary path, arguments, and install status.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLSPStatus()
	},
}

func runLSPList() error {
	servers := loadLanguageServers()

	// Column widths
	languageW := 12
	binaryW := 30
	statusW := 10

	// Print header
	fmt.Printf("%-*s  %-*s  %-*s\n", languageW, "Language", binaryW, "Server Binary", statusW, "Status")
	fmt.Printf("%-*s  %-*s  %-*s\n", languageW, strings.Repeat("-", languageW), binaryW, strings.Repeat("-", binaryW), statusW, strings.Repeat("-", statusW))

	for _, s := range servers {
		language := strings.Join(s.LanguageIDs, ", ")
		status := langServerStatus(s)
		fmt.Printf("%-*s  %-*s  %-*s\n", languageW, language, binaryW, s.Binary, statusW, status)
	}

	return nil
}

func runLSPInstall(language string) error {
	servers := loadLanguageServers()

	normalized := proxy.NormalizeLanguageID(language)
	server := proxy.FindLanguageServer(normalized, servers)
	// If not found by LanguageID, try matching by server ID (e.g., "shell", "go")
	if server == nil {
		server = proxy.FindLanguageServerByID(normalized, servers)
	}

	if server == nil {
		fmt.Fprintf(os.Stderr, "Language server for '%s' not found.\n\n", language)
		fmt.Println("Available languages:")
		for _, s := range servers {
			fmt.Printf("  - %s (%s)\n", strings.Join(s.LanguageIDs, ", "), s.Binary)
		}
		return fmt.Errorf("language '%s' not supported", language)
	}

	fmt.Println("Language Server Installation")
	fmt.Println("=============================")
	fmt.Printf("Language: %s\n", strings.Join(server.LanguageIDs, ", "))
	fmt.Printf("Binary:   %s\n", server.Binary)
	fmt.Printf("Args:     %v\n", server.Args)
	fmt.Println()
	fmt.Println("Installation instructions:")
	fmt.Printf("  %s\n", server.InstallHint)
	fmt.Println()

	// Check if already installed
	path, err := proxy.ResolveBinaryPath(server.Binary)
	if err == nil {
		fmt.Printf("✓ %s is already installed at: %s\n", server.Binary, path)
	} else {
		fmt.Printf("✗ %s is not installed.\n", server.Binary)
	}

	return nil
}

func runLSPStatus() error {
	servers := loadLanguageServers()

	fmt.Println("Language Server Status")
	fmt.Println("======================")
	fmt.Println()

	for _, s := range servers {
		language := strings.Join(s.LanguageIDs, ", ")
		status := langServerStatus(s)

		fmt.Printf("Language:  %s\n", language)
		fmt.Printf("Server:    %s\n", s.ID)
		fmt.Printf("Binary:    %s\n", s.Binary)
		fmt.Printf("Args:      %v\n", s.Args)

		path, err := proxy.ResolveBinaryPath(s.Binary)
		if err == nil {
			fmt.Printf("Path:      %s\n", path)
		} else {
			fmt.Printf("Path:      (not found)\n")
		}

		fmt.Printf("Status:    %s\n", status)
		fmt.Printf("Install:   %s\n", s.InstallHint)
		fmt.Println()
	}

	return nil
}

// loadLanguageServers loads the merged list of default + user-configured language servers.
func loadLanguageServers() []proxy.LanguageServerConfig {
	defaults := proxy.DefaultLanguageServers()

	cfg, err := configuration.LoadOrInitConfig(false)
	if err != nil || cfg == nil {
		return defaults
	}

	return proxy.MergeServers(defaults, cfg.LanguageServers)
}

// langServerStatus returns a human-readable status string for a language server.
func langServerStatus(s proxy.LanguageServerConfig) string {
	_, err := proxy.ResolveBinaryPath(s.Binary)
	if err == nil {
		return "installed"
	}
	return "not found"
}

func init() {
	lspCmd.AddCommand(lspListCmd)
	lspCmd.AddCommand(lspInstallCmd)
	lspCmd.AddCommand(lspStatusCmd)
	rootCmd.AddCommand(lspCmd)
}
