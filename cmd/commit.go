package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/spf13/cobra"
	"golang.org/x/term"
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

		// Check for staged changes
		if err := git.CheckStagedChanges(); err != nil {
			logger.LogUserInteraction(err.Error())
			return
		}
		logger.LogProcessStep("Staged changes detected. Generating commit message...")

		// Get the diff of staged changes
		stagedDiff, err := git.GetStagedDiff()
		if err != nil {
			logger.LogError(err)
			return
		}

		// Security check on staged files
		if cfg.EnableSecurityChecks {
			logger.LogProcessStep("Checking staged files for security credentials (added lines only)...")
			securityIssuesFound := git.CheckStagedFilesForSecurityCredentials(logger, cfg)
			if securityIssuesFound {
				if commitAllowSecrets {
					logger.LogProcessStep("Security issues detected but proceeding due to --allow-secrets override.")
				} else if !commitSkipPrompt {
					logger.LogUserInteraction("Security issues detected in staged files. Do you want to proceed with commit? (y/n): ")
					reader := bufio.NewReader(os.Stdin)
					userInput, _ := reader.ReadString('\n')
					userInput = strings.TrimSpace(strings.ToLower(userInput))
					if userInput != "y" && userInput != "yes" {
						logger.LogUserInteraction("Commit aborted due to security concerns.")
						return
					}
				} else {
					logger.LogProcessStep("Security issues detected but proceeding due to --skip-prompt flag.")
				}
			}
		}

		// Auto-detect non-interactive environment and force skip-prompt mode
		if !commitSkipPrompt && !term.IsTerminal(int(os.Stdin.Fd())) {
			logger.LogProcessStep("Non-interactive environment detected. Automatically committing with generated message.")
			commitSkipPrompt = true
		}

		reader := bufio.NewReader(os.Stdin)

		for {
			generatedMessage, err := llm.GetCommitMessage(cfg, stagedDiff, "Generate a commit message for staged changes.", "")
			if err != nil {
				logger.LogError(fmt.Errorf("failed to generate commit message: %w", err))
				logger.LogUserInteraction("Failed to generate commit message. Retrying...")
				continue
			}

			// Clean up the message using shared utility
			generatedMessage = git.CleanCommitMessage(generatedMessage)

			if commitDryRun {
				fmt.Printf("DRY RUN - Generated commit message:\n%s\n", generatedMessage)
				return
			}

			if commitSkipPrompt {
				logger.LogProcessStep(fmt.Sprintf("Skipping prompt. Committing with generated message:\n%s", generatedMessage))
				if err := git.PerformGitCommit(generatedMessage); err != nil {
					logger.LogError(err)
				}
				return
			}

			logger.LogUserInteraction(fmt.Sprintf("\nGenerated Commit Message:\n---\n%s\n---\n", generatedMessage))
			logger.LogUserInteraction("Confirm commit? (y/n/e to edit/r to retry): ")
			userInput, _ := reader.ReadString('\n')
			userInput = strings.TrimSpace(strings.ToLower(userInput))

			switch userInput {
			case "y", "yes":
				if err := git.PerformGitCommit(generatedMessage); err != nil {
					logger.LogError(err)
				}
				return
			case "n", "no":
				logger.LogUserInteraction("Commit aborted.")
				return
			case "e", "edit":
				editedMessage, err := editor.OpenInEditor(generatedMessage, ".gitmessage")
				if err != nil {
					logger.LogError(fmt.Errorf("failed to open editor: %w", err))
					logger.LogUserInteraction("Error opening editor. Retrying commit message generation.")
					continue
				}
				generatedMessage = editedMessage
				logger.LogUserInteraction(fmt.Sprintf("\nEdited Commit Message:\n---\n%s\n---\n", generatedMessage))
				logger.LogUserInteraction("Confirm edited commit? (y/n/r to retry generation): ")
				editConfirmInput, _ := reader.ReadString('\n')
				editConfirmInput = strings.TrimSpace(strings.ToLower(editConfirmInput))

				switch editConfirmInput {
				case "y", "yes":
					if err := git.PerformGitCommit(generatedMessage); err != nil {
						logger.LogError(err)
					}
					return
				case "n", "no":
					logger.LogUserInteraction("Commit aborted after edit.")
					return
				case "r", "retry":
					logger.LogUserInteraction("Retrying commit message generation...")
				default:
					logger.LogUserInteraction("Invalid input. Retrying commit message generation.")
				}
			case "r", "retry":
				logger.LogUserInteraction("Retrying commit message generation...")
			default:
				logger.LogUserInteraction("Invalid input. Please choose 'y', 'n', 'e', or 'r'.")
			}
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
