package cmd

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/spf13/cobra"
)

// enableUI controls whether to enable interactive UI mode
var enableUI bool

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ledit",
	Short: "AI agent for code analysis and editing (interactive mode when run without arguments)",
	Long: `Ledit is a command-line tool that leverages Large Language Models (LLMs)
to automate and assist in software development tasks. It can understand your
entire workspace, generate code, orchestrate complex features, and ground its
responses with live web search results.

Available commands:
  agent    - AI agent mode (analyzes intent and decides actions)
  shell    - Generate shell scripts from natural language descriptions
  commit   - Generate commit messages
  review-staged - Review staged changes
  log      - View operation logs
  mcp      - Manage MCP (Model Context Protocol) servers

For autonomous operation, try: ledit agent "your intent here"

Running just 'ledit' without arguments starts the interactive agent mode.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize API keys and configuration
		initializeSystem()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		useInteractive := enableUI || os.Getenv("LEDIT_UI") == "1"
		if !useInteractive && len(args) == 0 && cmd.Flags().NFlag() == 0 {
			useInteractive = true
		}
		if useInteractive {
			chatAgent, err := createChatAgent()
			if err != nil {
				return fmt.Errorf("failed to initialize agent for interactive mode: %w", err)
			}
			return runInteractiveMode(chatAgent)
		}
		// Otherwise show help
		return cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

// initializeSystem initializes configuration and API keys with first-run setup
func initializeSystem() {
	// Check if we're in a CI environment or non-interactive mode
	isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

	if isCI {
		// In CI environments, just load what we can and continue
		_, err := configuration.LoadAPIKeys()
		if err != nil && os.Getenv("LEDIT_DEBUG") != "" {
			println("API key initialization warning:", err.Error())
		}
		return
	}

	// For interactive use, ensure proper initialization
	_, _, err := configuration.Initialize()
	if err != nil {
		// If initialization fails, print helpful error and exit
		fmt.Fprintf(os.Stderr, "❌ Failed to initialize ledit: %v\n", err)
		fmt.Fprintln(os.Stderr, "\n💡 This usually means there's an issue with your configuration or API keys.")
		fmt.Fprintln(os.Stderr, "   Try running 'ledit' again to set up your AI provider.")
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be available to all subcommands in the application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ledit.yaml)")
	rootCmd.PersistentFlags().BoolVar(&enableUI, "ui", false, "Enable interactive terminal UI (or set LEDIT_UI=1)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(reviewStagedCmd)
	rootCmd.AddCommand(shellCmd)

	// Initialize environment-based defaults
	cobra.OnInitialize(func() {
		// UI is removed, nothing to initialize
	})
}
