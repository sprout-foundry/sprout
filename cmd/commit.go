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
	commitProvider     string
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
		if spec := providerModelSpec(commitProvider, commitModel); spec != "" {
			chatAgent, err = agent.NewAgentWithModel(spec)
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
			cmdArgs = append(cmdArgs, "--yes")
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
	commitCmd.Flags().BoolVarP(&commitSkipPrompt, "yes", "y", false, "Skip confirmation prompts and commit automatically")
	boolFlagAlias(commitCmd.Flags(), &commitSkipPrompt, "skip-prompt", "yes", aliasSilent)
	commitCmd.Flags().StringVarP(&commitModel, "model", "m", "", "Model for commit message generation (e.g. 'ollama:llama3')")
	commitCmd.Flags().StringVarP(&commitProvider, "provider", "p", "", providerFlagUsage)
	commitCmd.Flags().BoolVar(&commitAllowSecrets, "allow-secrets", false, "Allow committing files flagged as potentially containing secrets")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "Generate and display commit message without executing commit")
}
