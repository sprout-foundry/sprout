// Shell command for ledit
package cmd

import (
	"github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/spf13/cobra"
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

The generated script will be displayed for you to copy, save, or execute as needed.
No automatic execution occurs - you have full control over when and how to run the script.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runShellCommand,
}

func runShellCommand(cmd *cobra.Command, args []string) error {
	// Create a shell command instance and delegate to it
	shellCommand := &commands.ShellCommand{}

	// Create a mock agent (not used by ShellCommand.Execute since it creates its own)
	return shellCommand.Execute(args, nil)
}
