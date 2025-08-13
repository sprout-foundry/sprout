package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/security"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/spf13/cobra"
)

var (
	commitSkipPrompt   bool
	commitModel        string
	commitAllowSecrets bool
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
		cmdCheckStaged := exec.Command("git", "diff", "--cached", "--quiet", "--exit-code")
		if err := cmdCheckStaged.Run(); err != nil {
			// If err is not nil, it means there are staged changes (exit code 1) or another error
			if _, ok := err.(*exec.ExitError); ok {
				// ExitError means git exited with a non-zero status, which is what we want for staged changes
				logger.LogProcessStep("Staged changes detected. Generating commit message...")
			} else {
				logger.LogError(fmt.Errorf("failed to check for staged changes: %w", err))
				return
			}
		} else {
			logger.LogUserInteraction("No staged changes found. Please stage your changes before running 'ledit commit'.")
			return
		}

		// Get the diff of staged changes
		cmdDiff := exec.Command("git", "diff", "--cached")
		stagedDiffBytes, err := cmdDiff.Output()
		if err != nil {
			logger.LogError(fmt.Errorf("failed to get staged diff: %w", err))
			return
		}
		stagedDiff := string(stagedDiffBytes)

		// Security check on staged files
		if cfg.EnableSecurityChecks {
			logger.LogProcessStep("Checking staged files for security credentials...")
			securityIssuesFound := checkStagedFilesForSecurityCredentials(logger, cfg)
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

		reader := bufio.NewReader(os.Stdin)

		for {
			generatedMessage, err := llm.GetCommitMessage(cfg, stagedDiff, "Generate a commit message for staged changes.", "")
			if err != nil {
				logger.LogError(fmt.Errorf("failed to generate commit message: %w", err))
				logger.LogUserInteraction("Failed to generate commit message. Retrying...")
				continue // Retry generation
			}

			// Clean up the message: remove markdown fences if present
			if strings.HasPrefix(generatedMessage, "```") && strings.HasSuffix(generatedMessage, "```") {
				generatedMessage = strings.TrimPrefix(generatedMessage, "```")
				generatedMessage = strings.TrimSuffix(generatedMessage, "```")
				// Remove language specifier if present (e.g., "git")
				if strings.HasPrefix(generatedMessage, "git\n") {
					generatedMessage = strings.TrimPrefix(generatedMessage, "git\n")
				}
				generatedMessage = strings.TrimSpace(generatedMessage)
			}

			if commitSkipPrompt {
				logger.LogProcessStep(fmt.Sprintf("Skipping prompt. Committing with generated message:\n%s", generatedMessage))
				if err := performGitCommit(generatedMessage); err != nil {
					logger.LogError(fmt.Errorf("failed to commit changes: %w", err))
				}
				return
			}

			logger.LogUserInteraction(fmt.Sprintf("\nGenerated Commit Message:\n---\n%s\n---\n", generatedMessage))
			logger.LogUserInteraction("Confirm commit? (y/n/e to edit/r to retry): ")
			userInput, _ := reader.ReadString('\n')
			userInput = strings.TrimSpace(strings.ToLower(userInput))

			switch userInput {
			case "y", "yes":
				if err := performGitCommit(generatedMessage); err != nil {
					logger.LogError(fmt.Errorf("failed to commit changes: %w", err))
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
					continue // Go back to regenerate or re-prompt
				}
				generatedMessage = editedMessage // Use the edited message for confirmation
				// After editing, re-prompt for confirmation (y/n/r)
				logger.LogUserInteraction(fmt.Sprintf("\nEdited Commit Message:\n---\n%s\n---\n", generatedMessage))
				logger.LogUserInteraction("Confirm edited commit? (y/n/r to retry generation): ")
				editConfirmInput, _ := reader.ReadString('\n')
				editConfirmInput = strings.TrimSpace(strings.ToLower(editConfirmInput))

				switch editConfirmInput {
				case "y", "yes":
					if err := performGitCommit(generatedMessage); err != nil {
						logger.LogError(fmt.Errorf("failed to commit changes: %w", err))
					}
					return
				case "n", "no":
					logger.LogUserInteraction("Commit aborted after edit.")
					return
				case "r", "retry":
					logger.LogUserInteraction("Retrying commit message generation...")
					// Loop will continue and regenerate
				default:
					logger.LogUserInteraction("Invalid input. Retrying commit message generation.")
					// Loop will continue and regenerate
				}
			case "r", "retry":
				logger.LogUserInteraction("Retrying commit message generation...")
				// Loop will continue and regenerate
			default:
				logger.LogUserInteraction("Invalid input. Please choose 'y', 'n', 'e', or 'r'.")
			}
		}
	},
}

// checkStagedFilesForSecurityCredentials checks staged files for security credentials
func checkStagedFilesForSecurityCredentials(logger *utils.Logger, cfg *config.Config) bool {
	// Get list of staged files
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	output, err := cmd.Output()
	if err != nil {
		logger.LogError(fmt.Errorf("failed to get staged files: %w", err))
		return false
	}

	stagedFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
	securityIssuesFound := false

	for _, filePath := range stagedFiles {
		if filePath == "" {
			continue
		}

		// Get the content of staged changes for this file
		cmd := exec.Command("git", "show", ":"+filePath)
		contentBytes, err := cmd.Output()
		if err != nil {
			logger.LogError(fmt.Errorf("failed to get content of staged file %s: %w", filePath, err))
			continue
		}

		content := string(contentBytes)
		concerns, _ := security.DetectSecurityConcerns(content)

		if len(concerns) > 0 {
			securityIssuesFound = true
			logger.LogUserInteraction(fmt.Sprintf("Security concerns detected in staged file %s:", filePath))
			for _, concern := range concerns {
				logger.LogUserInteraction(fmt.Sprintf("  - %s", concern))
			}
		}
	}

	return securityIssuesFound
}

func performGitCommit(message string) error {
	// Git commit command requires the message to be passed as an argument to -m
	// If the message contains newlines, it might cause issues.
	// A common way to handle multi-line messages is to write to a file and use -F.
	// However, for simplicity and common use cases, we'll try -m first.
	// If the message is truly multi-line, git will usually handle it if quoted properly.
	// Let's ensure the message is treated as a single argument.

	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	fmt.Println("Changes committed successfully.")
	return nil
}

func init() {
	commitCmd.Flags().BoolVar(&commitSkipPrompt, "skip-prompt", false, "Skip confirmation prompts and automatically commit")
	commitCmd.Flags().StringVar(&commitModel, "model", "", "Specify the LLM model to use for commit message generation (e.g., 'ollama:llama3')")
	commitCmd.Flags().BoolVar(&commitAllowSecrets, "allow-secrets", false, "Allow committing even if potential secrets are detected (override)")
	rootCmd.AddCommand(commitCmd)
}
