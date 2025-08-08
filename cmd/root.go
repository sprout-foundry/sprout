package cmd

import (
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
  fix      - Fix common issues
  ...and more

For autonomous operation, try: ledit agent "your intent here"`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be available to all subcommands in the application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ledit.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(codeCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(fixCmd)
	rootCmd.AddCommand(ignoreCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(processCmd)
	rootCmd.AddCommand(questionCmd)
	rootCmd.AddCommand(reviewStagedCmd) // Add the new command
}
