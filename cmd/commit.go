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
	commitTrailers     []string
)

// buildCommitArgs maps the `sprout commit` flags onto the argument list the
// /commit command parser understands. Each --trailer value is passed as a
// separate --trailer argument (the value may contain spaces).
func buildCommitArgs(skipPrompt, dryRun, allowSecrets bool, trailers []string) []string {
	var args []string
	if skipPrompt {
		args = append(args, "--yes")
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	if allowSecrets {
		args = append(args, "--allow-secrets")
	}
	for _, trailer := range trailers {
		args = append(args, "--trailer", trailer)
	}
	return args
}

// newCommitAgent creates the commit message agent. Inside a git repository
// (auto-detected or --isolated-config) it uses the layered config manager so
// the repo's .sprout settings (commit_trailers, commit provider, roles)
// apply; elsewhere the plain global-config agent is used.
func newCommitAgent(spec string) (*agent.Agent, error) {
	if autoDetectedWorkspaceDir != "" && resolveGlobalConfigDir() != "" {
		return agent.NewAgentWithLayers(resolveGlobalConfigDir(), autoDetectedWorkspaceDir, spec)
	}
	if spec != "" {
		return agent.NewAgentWithModel(spec)
	}
	return agent.NewAgent()
}

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate a commit message and complete a git commit for staged changes",
	Long: `This command generates a conventional git commit message based on your staged changes
and then allows you to confirm, edit, or retry the commit before finalizing it.

Git trailers ("Key: Value" lines) can be appended to the commit message with
repeatable --trailer flags. Default trailers come from the commit.trailers
setting (the .sprout config) and the repository's AGENTS.md "Commit
Trailers" section; they are written with git interpret-trailers semantics —
a blank line before a new trailer block, identical trailers never
duplicated — and appear in the message --dry-run displays.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := utils.GetLogger(commitSkipPrompt)

		_, err := configuration.LoadOrInitConfig(commitSkipPrompt)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to load or initialize config: %w", err))
		}

		var chatAgent *agent.Agent
		spec := providerModelSpec(commitProvider, commitModel)
		chatAgent, err = newCommitAgent(spec)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to create agent: %w", err))
			chatAgent = nil
		}

		commitCmd := &commands.CommitCommand{}

		if chatAgent == nil && err != nil {
			commitCmd.SetAgentError(err)
		}

		cmdArgs := buildCommitArgs(commitSkipPrompt, commitDryRun, commitAllowSecrets, commitTrailers)

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
	commitCmd.Flags().StringArrayVar(&commitTrailers, "trailer", nil, "Git trailer (\"Key: Value\") appended to the commit message; repeatable, and joined with the commit.trailers defaults from the .sprout config and the repo's AGENTS.md")
}
