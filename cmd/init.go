package cmd

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"

	"github.com/spf13/cobra"
)

// TODO: Migrate to new BaseCommand framework
var initSkipPrompt bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new configuration in the current directory",
	Long:  `Creates a .ledit/config.json file in the current working directory, allowing for project-specific settings.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Initializing new configuration in the current directory...")
		if err := config.InitConfig(initSkipPrompt); err != nil {
			gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
				"Initializing configuration",
				err,
				nil, // No token usage for config initialization
				"",
			)
			fmt.Fprint(os.Stderr, gracefulExitMsg)
			os.Exit(1)
		}

		// Generate validation context for the project
		fmt.Println("Analyzing project for validation requirements...")
		if err := agent.RegenerateProjectValidationContext(); err != nil {
			fmt.Printf("Warning: Could not generate validation context: %v\n", err)
			// Don't exit - this is not critical for init
		} else {
			fmt.Println("✓ Generated validation context")
		}
	},
}

func init() {
	initCmd.Flags().BoolVar(&initSkipPrompt, "skip-prompt", false, "Skip user confirmation prompts")
}
