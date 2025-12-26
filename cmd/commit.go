package cmd

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/configuration"
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

		_, err := configuration.LoadOrInitConfig(commitSkipPrompt)
		if err != nil {
			// Non-fatal for commit flow; continue with manual fallback if needed
			logger.LogError(fmt.Errorf("failed to load or initialize config: %w", err))
		}

		// Create agent instance for commit processing
		var chatAgent *agent.Agent
		if commitModel != "" {
			// Create agent with specified model
			chatAgent, err = agent.NewAgentWithModel(commitModel)
		} else {
			chatAgent, err = agent.NewAgent()
		}
		if err != nil {
			// Proceed without agent; the commit flow will fall back to manual prompts
			logger.LogError(fmt.Errorf("failed to create agent (falling back to manual mode): %w", err))
			chatAgent = nil
		}

		// Create commit command instance and execute
		commitCmd := &commands.CommitCommand{}

		// If agent creation failed, set the error for better reporting
		if chatAgent == nil && err != nil {
			commitCmd.SetAgentError(err)
		}

		// Parse CLI flags into appropriate args for the agent command
		var cmdArgs []string

		// Pass flags through to the agent command
		if commitSkipPrompt {
			cmdArgs = append(cmdArgs, "--skip-prompt")
		}
		if commitDryRun {
			cmdArgs = append(cmdArgs, "--dry-run")
		}
		if commitAllowSecrets {
			cmdArgs = append(cmdArgs, "--allow-secrets")
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
}
