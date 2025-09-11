package cmd

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/spf13/cobra"
)

var (
	commitSkipPrompt   bool
	commitModel        string
	commitAllowSecrets bool
	commitDryRun       bool
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate a commit message and complete a git commit for staged changes",
	Long: `This command generates a conventional git commit message based on your staged changes
and then allows you to confirm, edit, or retry the commit before finalizing it.`,
	Run: func(cmd *cobra.Command, args []string) {
		logger := utils.GetLogger(commitSkipPrompt)

		cfg, err := config.LoadOrInitConfig(commitSkipPrompt)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to load or initialize config: %w", err))
			return
		}

		// Override model if specified by flag
		if commitModel != "" {
			cfg.WorkspaceModel = commitModel
		}

		// Create agent instance for commit processing
		chatAgent, err := agent.NewAgent()
		if err != nil {
			logger.LogError(fmt.Errorf("failed to create agent: %w", err))
			return
		}

		// Create commit command instance and execute
		commitCmd := &commands.CommitCommand{}

		// Parse CLI flags into appropriate args for the agent command
		var cmdArgs []string

		// Handle dry run mode (not supported by agent command, but we can warn)
		if commitDryRun {
			logger.LogUserInteraction("Note: --dry-run flag not supported in agent-based commit. Use interactive confirmation instead.")
		}

		// Handle skip prompt mode (not directly supported, but agent command provides y/n options)
		if commitSkipPrompt {
			logger.LogUserInteraction("Note: --skip-prompt flag not supported in agent-based commit. Use interactive y/n confirmation.")
		}

		// Handle allow secrets flag (not directly supported in agent command)
		if commitAllowSecrets {
			logger.LogUserInteraction("Note: --allow-secrets flag not supported in agent-based commit.")
		}

		// Execute the unified commit workflow
		err = commitCmd.Execute(cmdArgs, chatAgent)
		if err != nil {
			logger.LogError(fmt.Errorf("commit failed: %w", err))
		}
	},
}

func init() {
	commitCmd.Flags().BoolVar(&commitSkipPrompt, "skip-prompt", false, "Skip confirmation prompts and automatically commit")
	commitCmd.Flags().StringVar(&commitModel, "model", "", "Specify the LLM model to use for commit message generation (e.g., 'ollama:llama3')")
	commitCmd.Flags().BoolVar(&commitAllowSecrets, "allow-secrets", false, "Allow committing even if potential secrets are detected (override)")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "Generate and display the commit message without executing the commit")
	rootCmd.AddCommand(commitCmd)
}
