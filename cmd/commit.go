//go:build !js

package cmd

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent"
	commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"

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
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := utils.GetLogger(commitSkipPrompt)

		_, err := configuration.LoadOrInitConfig(commitSkipPrompt)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to load or initialize config: %w", err))
		}

		var chatAgent *agent.Agent
		if commitModel != "" {
			chatAgent, err = agent.NewAgentWithModel(commitModel)
		} else {
			chatAgent, err = agent.NewAgent()
		}
		if err != nil {
			logger.LogError(fmt.Errorf("failed to create agent: %w", err))
			chatAgent = nil
		}

		commitCmd := &commands.CommitCommand{}

		if chatAgent == nil && err != nil {
			commitCmd.SetAgentError(err)
		}

		var cmdArgs []string
		if commitSkipPrompt {
			cmdArgs = append(cmdArgs, "--skip-prompt")
		}
		if commitDryRun {
			cmdArgs = append(cmdArgs, "--dry-run")
		}
		if commitAllowSecrets {
			cmdArgs = append(cmdArgs, "--allow-secrets")
		}

		err = commitCmd.Execute(cmdArgs, chatAgent)
		if err != nil {
			logger.LogError(fmt.Errorf("commit failed: %w", err))
			return err
		}
		return nil
	},
}

func init() {
	commitCmd.Flags().BoolVar(&commitSkipPrompt, "skip-prompt", false, "Skip confirmation prompts and automatically commit")
	commitCmd.Flags().StringVar(&commitModel, "model", "", "Specify LLM model to use for commit message generation (e.g., 'ollama:llama3')")
	commitCmd.Flags().BoolVar(&commitAllowSecrets, "allow-secrets", false, "Allow committing files flagged as potentially containing secrets")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "Generate and display commit message without executing commit")
}
