//go:build !windows

package cmd

import (
	"os"

	agent_api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/ui"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ledit",
	Short: "AI-powered code editor and orchestrator",
	Long: `Ledit is a command-line tool that leverages Large Language Models (LLMs)
to automate and assist in software development tasks. It can understand your
entire workspace, generate code, orchestrate complex features, and ground its
responses with live web search results.

Available commands:
  code     - Generate/edit code based on instructions
  agent    - AI agent mode (analyzes intent and decides actions)
  process  - Orchestrate complex features
  commit   - Generate commit messages
  rollback - Rollback changes by revision ID
  insights - Show inferred project goals and insights
  ...and more

For autonomous operation, try: ledit agent "your intent here"`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize API keys and configuration
		initializeSystem()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

// initializeSystem initializes API keys and configuration on startup
func initializeSystem() {
	// Initialize API keys from ~/.ledit/api_keys.json
	if err := agent_api.InitializeAPIKeys(); err != nil {
		// Don't fail on API key initialization errors
		if os.Getenv("LEDIT_DEBUG") != "" {
			println("API key initialization warning:", err.Error())
		}
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be available to all subcommands in the application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ledit.yaml)")
	rootCmd.PersistentFlags().Bool("ui", false, "Enable interactive terminal UI (or set LEDIT_UI=1)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(codeCmd.GetCommand())
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(ignoreCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(reviewStagedCmd) // Add the new command
	rootCmd.AddCommand(uiCmd)

	// Initialize UI handling and environment-based defaults
	cobra.OnInitialize(func() {
		// consider env first
		if ui.FromEnv() {
			ui.SetEnabled(true)
			ui.SetDefaultSink(ui.TuiSink{})
			return
		}
		// then flag
		if f := rootCmd.PersistentFlags().Lookup("ui"); f != nil {
			if v, err := rootCmd.PersistentFlags().GetBool("ui"); err == nil && v {
				ui.SetEnabled(true)
				ui.SetDefaultSink(ui.TuiSink{})
				return
			}
		}
		// default off if non-interactive
		if os.Getenv("CI") != "" {
			ui.SetEnabled(false)
		}
	})
}
