package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/factory"
	gitops "github.com/sprout-foundry/sprout/pkg/git"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// --- Output helpers ---

// printf prints formatted output with proper newline handling
func (c *CommitCommand) printf(format string, args ...interface{}) {
	fmt.Fprint(os.Stdout, normalizeNewlines(fmt.Sprintf(format, args...)))
}

// println prints a line with proper newline handling
func (c *CommitCommand) println(text string) {
	fmt.Fprint(os.Stdout, normalizeNewlines(text)+"\r\n")
}

// --- Flag parsing ---

// parseFlags parses command-line flags and sets internal state
func (c *CommitCommand) parseFlags(args []string) []string {
	var cleanArgs []string
	for _, arg := range args {
		switch arg {
		case "--skip-prompt":
			c.skipPrompt = true
		case "--dry-run":
			c.dryRun = true
		case "--allow-secrets":
			c.allowSecrets = true
		default:
			cleanArgs = append(cleanArgs, arg)
		}
	}
	return cleanArgs
}

// --- Editor ---

// editInEditor opens $VISUAL or $EDITOR to edit content, returns the edited text
func editInEditor(initial string) (string, error) {
	// Create temp file
	f, err := os.CreateTemp("", "sprout_commit_*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	path := f.Name()
	_, _ = f.WriteString(initial)
	f.Close()

	// Choose editor
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Release stdin to cooked mode so the editor reads keystrokes
	// normally. No-op when no turn / steer reader is active (the
	// common slash-command path).
	if err := clihooks.WithCookedStdin(cmd.Run); err != nil {
		return "", fmt.Errorf("editor failed: %w", err)
	}

	// Read back
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// --- Command interface ---

// Usage returns the detailed help text shown by `/help commit`.
func (c *CommitCommand) Usage() string {
	return strings.Join([]string{
		"/commit [instructions] [--skip-prompt] [--dry-run] [--allow-secrets]",
		"",
		"Interactive commit workflow: stage files, generate an AI commit",
		"message from the staged diff, confirm, and create the commit.",
		"",
		"Flags:",
		"  --skip-prompt     Auto-proceed without the confirm picker.",
		"  --dry-run         Generate the message but do not commit.",
		"  --allow-secrets   Bypass the secret-detection security check.",
		"",
		"Examples:",
		`  /commit`,
		`  /commit "fix auth flow in login handler"`,
		`  /commit --dry-run`,
	}, "\n")
}

// Name returns the command name
func (c *CommitCommand) Name() string {
	return "commit"
}

// SafeDuringSteer returns false - /commit is git mutation + agent interaction
func (c *CommitCommand) SafeDuringSteer() bool {
	return false
}

// Description returns the command description
func (c *CommitCommand) Description() string {
	return "Interactive commit workflow with dropdown selection - stage files and generate commit messages"
}

// SetAgentError sets the agent creation error for better error reporting
func (c *CommitCommand) SetAgentError(err error) {
	c.agentError = err
}

// Execute runs the commit command
func (c *CommitCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// Set git working directory from agent workspace root
	if chatAgent != nil {
		SetGitDir(chatAgent.GetWorkspaceRoot())
	} else {
		SetGitDir("")
	}
	// Parse flags and get clean args
	cleanArgs := c.parseFlags(args)

	// Store any remaining args as user instructions
	if len(cleanArgs) > 0 {
		c.userInstructions = strings.Join(cleanArgs, " ")
		// Check for help command first
		if c.userInstructions == "help" || c.userInstructions == "--help" || c.userInstructions == "-h" {
			return c.showHelp()
		}
	}

	// Default behavior: use new interactive commit flow with flags
	flow := NewCommitFlowWithFlags(chatAgent, c.skipPrompt, c.dryRun, c.allowSecrets)
	flow.SetUserInstructions(c.userInstructions)
	return flow.Execute()
}

// ExecuteWithJSONOutput runs the commit command with JSON output
func (c *CommitCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	// Parse flags using helper
	cleanArgs := c.parseFlags(args)
	if len(cleanArgs) > 0 {
		c.userInstructions = strings.Join(cleanArgs, " ")
	}

	// Run commit flow
	flow := NewCommitFlowWithFlags(chatAgent, c.skipPrompt, c.dryRun, c.allowSecrets)
	flow.SetUserInstructions(c.userInstructions)
	if err := flow.Execute(); err != nil {
		result := CommitJSONResult{
			Status: CommitStatusError,
			Error:  err.Error(),
		}
		if err := result.Validate(); err != nil {
			return fmt.Errorf("JSON validation failed: %w", err)
		}
		return WriteJSONToOutput(result)
	}

	// Handle dry-run mode
	if c.dryRun {
		result := CommitJSONResult{
			Status:  CommitStatusDryRun,
			Message: "Dry-run mode: commit message generated successfully without creating commit",
		}
		if err := result.Validate(); err != nil {
			return fmt.Errorf("JSON validation failed: %w", err)
		}
		return WriteJSONToOutput(result)
	}

	// Get commit hash using helper
	commitHash, err := getGitCommitHash()
	if err != nil {
		return WriteJSONToOutput(CommitJSONResult{
			Status: CommitStatusError,
			Error:  err.Error(),
		})
	}

	// Get branch name using helper
	branch, err := getGitBranchName()
	if err != nil {
		return WriteJSONToOutput(CommitJSONResult{
			Status: CommitStatusSuccess,
			Commit: commitHash,
		})
	}

	result := CommitJSONResult{
		Status: CommitStatusSuccess,
		Commit: commitHash,
		Branch: branch,
	}
	if err := result.Validate(); err != nil {
		return fmt.Errorf("JSON validation failed: %w", err)
	}
	return WriteJSONToOutput(result)
}

// --- File selection and staging ---

func (c *CommitCommand) selectAndStageFiles(chatAgent *agent.Agent, reader *bufio.Reader) ([]string, error) {
	validStatusLines, err := getPorcelainStatusLines()
	if err != nil {
		return nil, fmt.Errorf("failed to get git status: %w", err)
	}
	var filesToAdd []string
	fmt.Println()
	console.GlyphInfo.Print("Enter file numbers to commit (comma-separated, 'a' for all, 'q' to quit):")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	switch strings.ToLower(input) {
	case "q", "quit":
		console.GlyphError.Print("Commit cancelled")
		return nil, nil
	case "a", "all":
		filesToAdd = selectAllModifiedFiles(validStatusLines)
		console.GlyphSuccess.Print("Adding all modified files")
	default:
		selections := strings.Split(input, ",")
		for _, sel := range selections {
			sel = strings.TrimSpace(sel)
			if sel == "" {
				continue
			}
			var index int
			if _, err := fmt.Sscanf(sel, "%d", &index); err != nil || index < 1 || index > len(validStatusLines) {
				console.GlyphError.Printf("Invalid selection: %s", sel)
				continue
			}
			if name, ok := parseFilenameFromStatusLine(validStatusLines[index-1]); ok {
				filesToAdd = append(filesToAdd, name)
				console.GlyphSuccess.Printf("Adding: %s", name)
			}
		}
	}

	// Stage files
	for _, file := range filesToAdd {
		cmd := gitCommand("add", file)
		output, err := cmd.CombinedOutput()
		if err != nil {
			c.printf("%sFailed to stage %s: %v\n", console.GlyphError.Prefix(), file, err)
			if len(output) > 0 {
				c.printf("Output: %s\n", string(output))
			}
		} else {
			c.printf("%sStaged: %s\n", console.GlyphSuccess.Prefix(), file)
		}
	}

	return filesToAdd, nil
}

func (c *CommitCommand) checkForAnyChanges(chatAgent *agent.Agent) (bool, error) {
	validStatusLines, err := getPorcelainStatusLines()
	if err != nil {
		return false, fmt.Errorf("get git status: %w", err)
	}
	if len(validStatusLines) == 0 {
		chatAgent.PrintLine(console.GlyphInfo.Prefix() + "No changes to commit")
		return false, nil
	}
	return true, nil
}

func (c *CommitCommand) printStatus(chatAgent *agent.Agent) error {
	validStatusLines, err := getPorcelainStatusLines()
	if err != nil {
		chatAgent.PrintLine("Failed to get git status")
		return fmt.Errorf("printStatus: %w", err)
	}
	// Print the current git status
	chatAgent.PrintLine(console.GlyphInfo.Prefix() + "Current git status:")
	chatAgent.PrintLine("\nModified files:")
	for i, line := range validStatusLines {
		chatAgent.PrintLine(fmt.Sprintf("%2d. %s", i+1, line))
	}

	return nil
}

// executeMultiFileCommit handles the original multi-file commit workflow
func (c *CommitCommand) executeMultiFileCommit(chatAgent *agent.Agent) error {
	if ok, err := c.checkForAnyChanges(chatAgent); !ok {
		return fmt.Errorf("executeMultiFileCommit: check changes: %w", err)
	}
	reader := bufio.NewReader(os.Stdin)

	// Step 1: Check for staged files first
	staged, err := getStagedFiles()
	if err != nil {
		return fmt.Errorf("executeMultiFileCommit: get staged files: %w", err)
	}
	if len(staged) == 0 {
		chatAgent.PrintLine(console.GlyphInfo.Prefix() + "No staged files found")
		staged, err = c.selectAndStageFiles(chatAgent, reader)
		if err != nil {
			return fmt.Errorf("executeMultiFileCommit: select and stage files: %w", err)
		}
	} else {
		chatAgent.PrintLine(fmt.Sprintf("%sFound %d staged file(s):", console.GlyphInfo.Prefix(), len(staged)))
	}

	if err := c.printStatus(chatAgent); err != nil {
		return fmt.Errorf("executeMultiFileCommit: print status: %w", err)
	}

	if len(staged) == 0 {
		console.GlyphError.Print("No files selected")
		return nil
	}

	// Step 4: Stage the selected files
	return c.generateAndCommit(chatAgent, reader)
}

// showHelp displays commit command usage
func (c *CommitCommand) showHelp() error {
	console.GlyphInfo.Fprintln(os.Stdout, "Commit Command Usage:")
	fmt.Println("/commit          - Interactive commit workflow for staged files")
	fmt.Println("/commit help     - Show this help message")
	fmt.Println()
	fmt.Println("The interactive workflow helps you commit staged files")
	fmt.Println()
	return nil
}

// generateAndCommit handles commit message generation and commit creation
func (c *CommitCommand) generateAndCommit(chatAgent *agent.Agent, reader *bufio.Reader) error {
	// If reader is nil, create one
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}

	// Generate commit message
	c.println("")
	// Load configuration to get commit provider/model settings
	cfg, err := configuration.LoadOrInitConfig(true)
	if err != nil {
		c.printf("%sFailed to load configuration: %v\n", console.GlyphWarning.Prefix(), err)
		if chatAgent == nil {
			if c.agentError != nil {
				c.printf("%sUsing manual commit mode (AI agent unavailable: %v)", console.GlyphWarning.Prefix(), c.agentError)
			} else {
				c.println("Using manual commit mode (no AI agent available)")
			}
		}
	} else {
		commitProvider := cfg.GetCommitProvider()
		commitModel := cfg.GetCommitModel()
		c.printf("Using provider: %s, model: %s for commit message generation\n", commitProvider, commitModel)
	}

	// Get staged diff
	diffOutput, err := gitCommand("diff", "--staged").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get staged diff: %w", err)
	}

	if len(strings.TrimSpace(string(diffOutput))) == 0 {
		c.println(console.GlyphError.Prefix() + "No changes staged")
		return nil
	}

	// Prepare LLM client using configured commit provider/model
	var client api.ClientInterface
	var clientType api.ClientType
	var model string

	// Use configured commit provider/model from config if available
	if cfg != nil && cfg.GetCommitProvider() != "" {
		clientType = api.ClientType(cfg.GetCommitProvider())
		model = cfg.GetCommitModel()
		if cl, ce := factory.CreateProviderClient(clientType, model); ce == nil {
			client = cl
		}
	}

	// Fall back to chatAgent's config if client not created
	if client == nil && chatAgent != nil {
		configManager := chatAgent.GetConfigManager()
		if configManager != nil {
			if ct, e := configManager.GetProvider(); e == nil {
				clientType = ct
				model = configManager.GetModelForProvider(clientType)
				if cl, ce := factory.CreateProviderClient(clientType, model); ce == nil {
					client = cl
				}
			}
		}
	}

	// Get current branch name
	branchOutput, err := gitCommand("rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get branch name: %w", err)
	}
	branch := strings.TrimSpace(string(branchOutput))

	// Get staged files with their status
	stagedFilesOutput, err := gitCommand("diff", "--cached", "--name-status").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get staged files status: %w", err)
	}

	// Parse file actions and filenames
	fileChanges := make([]gitops.CommitFileChange, 0)
	stagedFilenames := []string{}
	lines := strings.Split(strings.TrimSpace(string(stagedFilesOutput)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			status := parts[0]
			filepath := strings.Join(parts[1:], " ")
			stagedFilenames = append(stagedFilenames, filepath)
			fileChanges = append(fileChanges, gitops.CommitFileChange{
				Status: status,
				Path:   filepath,
			})
		}
	}

	var commitMessage string

	// Retry loop for commit message generation (LLM if available, otherwise manual input)
retryLoop:
	for {
		if client == nil {
			// Manual fallback when LLM client isn't available
			c.println("")
			c.println(console.GlyphInfo.Prefix() + "Staged diff (truncated):")
			preview := string(diffOutput)
			if len(preview) > 2000 {
				preview = preview[:2000] + "\n... (truncated)"
			}
			c.println(preview)
			c.println("")
			c.println(console.GlyphAction.Prefix() + "Enter commit message (end with a blank line):")
			var b strings.Builder
			empty := 0
			for {
				line, _ := reader.ReadString('\n')
				if strings.TrimSpace(line) == "" {
					empty++
					if empty >= 1 {
						break
					}
				} else {
					empty = 0
				}
				b.WriteString(line)
			}
			commitMessage = strings.TrimSpace(b.String())
			if commitMessage == "" {
				c.println(console.GlyphError.Prefix() + "Empty commit message; aborting")
				return nil
			}

			break
		}
		result, err := gitops.GenerateCommitMessageFromStagedDiff(client, gitops.CommitMessageOptions{
			Diff:             string(diffOutput),
			Branch:           branch,
			FileChanges:      fileChanges,
			UserInstructions: c.userInstructions,
		})
		if err != nil {
			return fmt.Errorf("failed to generate commit message: %w", err)
		}
		commitMessage = result.Message
		c.printf("\n$ Tokens used: ~%d (model: %s/%s)\n", result.ApproxTokens, clientType, model)
		for _, warning := range result.Warnings {
			c.printf("%s%s\n", console.GlyphWarning.Prefix(), warning)
		}

		// Show staged files summary and commit message (minimal, no emoji)
		c.println("")
		if len(stagedFilenames) > 0 {
			c.printf("Committing %d staged file(s):\n", len(stagedFilenames))
			const maxList = 10
			for i, name := range stagedFilenames {
				if i >= maxList {
					remaining := len(stagedFilenames) - maxList
					if remaining > 0 {
						c.printf("... (+%d more)\n", remaining)
					}
					break
				}
				c.printf("- %s\n", name)
			}
		}
		c.println("")
		c.println("With message:")
		c.println("")
		c.println(commitMessage)
		c.println("")

		// Handle confirmation (or auto-proceed if skipPrompt)
		if c.skipPrompt {
			c.println("")
			c.println(console.GlyphSuccess.Prefix() + "Auto-proceeding with commit (--skip-prompt)")
			break // Exit retry loop
		}

		// Unified picker for the commit-confirm choice. Replaces the
		// prior dual-path (PromptChoice when AGENT_CONSOLE=1 / stdin
		// y/n/e/r loop otherwise) with a single SelectList that
		// degrades to numbered-list+stdin on non-TTY. The retry loop
		// stays — Retry re-enters the LLM call, Edit opens $EDITOR
		// and exits the retry loop.
		picker := console.NewSelectList(console.SelectListOptions{
			Title: "Proceed with commit?",
			Items: []console.SelectItem{
				{Label: "Approve", Detail: "create the commit now", Value: "y"},
				{Label: "Retry", Detail: "regenerate message", Value: "r"},
				{Label: "Edit", Detail: "open $EDITOR", Value: "e"},
				{Label: "Cancel", Detail: "abort", Value: "n"},
			},
			PageSize: 4,
		})
		choice, ok, perr := picker.Run(context.Background())
		if perr != nil {
			return fmt.Errorf("confirmation failed: %w", perr)
		}
		if !ok || choice == "n" {
			c.println("Commit cancelled")
			return nil
		}
		switch choice {
		case "r":
			c.println("Regenerating commit message...")
			continue
		case "e":
			edited, err := editInEditor(commitMessage)
			if err != nil {
				return fmt.Errorf("editor failed: %w", err)
			}
			if strings.TrimSpace(edited) == "" {
				c.println("Empty commit message; aborting")
				return nil
			}
			commitMessage = edited
			break retryLoop
		case "y":
			break retryLoop
		}

	} // End of retry loop

	// Handle dry-run mode
	if c.dryRun {
		c.println("")
		c.println(console.GlyphInfo.Prefix() + "Dry-run mode: Commit message generated successfully!")
		c.println(console.GlyphInfo.Prefix() + "The commit was not created due to --dry-run flag")
		c.println(console.GlyphAction.Prefix() + "To create the commit, run the command again without --dry-run")
		return nil
	}

	// Security check for staged files (unless --allow-secrets is set)
	if !c.allowSecrets && chatAgent != nil && chatAgent.GetElevationGate() != nil {
		gate := chatAgent.GetElevationGate()
		logger := utils.GetLogger(false)
		securityResult := gitops.CheckStagedFilesForSecurityCredentials(logger, chatAgent.GetWorkspaceRoot())
		if securityResult.HasConcerns && len(securityResult.Concerns) > 0 {
			action, err := gate.Evaluate(securityResult.Concerns, "commit")
			if err != nil {
				c.printf("%scommit elevation error: %v\n", console.GlyphError.Prefix(), err)
			}
			if err != nil || action == security.SecretBlock {
				c.println(console.GlyphError.Prefix() + "Commit aborted: detected secrets in staged files. Use --allow-secrets to override.")
				return nil
			}
			if action == security.SecretRedact {
				c.println(console.GlyphWarning.Prefix() + "Warning: commit proceeding but secrets were detected in staged files.")
			}
		}
	}

	// Create the commit
	c.println("")
	c.println(console.GlyphAction.Prefix() + "Creating commit...")

	// Write commit message to temporary file
	tempFile := "commit_msg.txt"
	err = os.WriteFile(tempFile, []byte(commitMessage), 0644)
	if err != nil {
		return fmt.Errorf("failed to create temporary commit message file: %w", err)
	}
	defer os.Remove(tempFile)

	cmd := gitCommand("commit", "-F", tempFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create commit: %w\nOutput: %s", err, string(output))
	}

	c.println(console.GlyphSuccess.Prefix() + "Commit created successfully!")
	c.printf("Output: %s\n", string(output))

	return nil
}
