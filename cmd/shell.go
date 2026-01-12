// Shell command for ledit
package cmd

import (
	"github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/spf13/cobra"
)

var (
	shellProvider string
	shellModel    string
)

var shellCmd = &cobra.Command{
	Use:   "shell [description]",
	Short: "Generate shell scripts from natural language descriptions",
	Long: `Generate shell scripts from natural language descriptions with full environmental context.

This command uses AI to generate complete, executable shell scripts based on your description.
The generated scripts are tailored to your current environment (OS, shell, available tools, etc.)
and include proper error handling and best practices.

Examples:
  ledit shell "backup all .go files to a timestamped archive"
  ledit shell "find and delete all node_modules directories older than 30 days"
  ledit shell "setup a development environment for a React project"
  ledit shell 'list all files larger than 100MB and sort by size'
  
  # With specific provider and model
  ledit shell --provider openrouter --model "qwen/qwen3-coder-30b" "backup all .go files"
  ledit shell -p deepinfra -m "deepseek-v3" "list all files larger than 100MB"

The generated script will be displayed for you to copy, save, or execute as needed.
No automatic execution occurs - you have full control over when and how to run the script.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runShellCommand,
}

func runShellCommand(cmd *cobra.Command, args []string) error {
	// Create a shell command instance and set provider/model
	shellCommand := &commands.ShellCommand{
		Provider: shellProvider,
		Model:    shellModel,
	}

	// Execute (uses agent's Execute method with args, chatAgent)
	return shellCommand.Execute(args, nil)
}

func init() {
	shellCmd.Flags().StringVarP(&shellProvider, "provider", "p", "", "Provider to use (openai, openrouter, deepinfra, deepseek, ollama, etc.)")
	shellCmd.Flags().StringVarP(&shellModel, "model", "m", "", "Model name (e.g., 'gpt-4', 'qwen/qwen3-coder-30b', 'deepseek-v3')")
}
