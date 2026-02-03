package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/utils"
	"golang.org/x/term"
)

// CommitFlow manages the interactive commit workflow
type CommitFlow struct {
	agent            *agent.Agent
	optimizer        *utils.DiffOptimizer
	skipPrompt       bool
	dryRun           bool
	allowSecrets     bool
	userInstructions string
}

// NewCommitFlow creates a new commit flow
func NewCommitFlow(chatAgent *agent.Agent) *CommitFlow {
	return &CommitFlow{
		agent:     chatAgent,
		optimizer: utils.NewDiffOptimizer(),
	}
}

// NewCommitFlowWithFlags creates a new commit flow with CLI flags
func NewCommitFlowWithFlags(chatAgent *agent.Agent, skipPrompt, dryRun, allowSecrets bool) *CommitFlow {
	return &CommitFlow{
		agent:        chatAgent,
		optimizer:    utils.NewDiffOptimizer(),
		skipPrompt:   skipPrompt,
		dryRun:       dryRun,
		allowSecrets: allowSecrets,
	}
}

// SetUserInstructions sets the user instructions for commit message generation
func (cf *CommitFlow) SetUserInstructions(instructions string) {
	cf.userInstructions = instructions
}

func (cf *CommitFlow) printf(format string, args ...interface{}) {
	fmt.Fprint(os.Stdout, normalizeNewlines(fmt.Sprintf(format, args...)))
}

func (cf *CommitFlow) println(text string) {
	fmt.Fprint(os.Stdout, normalizeNewlines(text)+"\r\n")
}

// CommitAction represents different commit actions
type CommitAction struct {
	ID          string
	DisplayName string
	Description string
	Action      func(*CommitFlow) error
}

// CommitActionItem adapts CommitAction for dropdown display
type CommitActionItem struct {
	ID          string
	DisplayName string
	Description string
}

func (c *CommitActionItem) Display() string    { return c.DisplayName }
func (c *CommitActionItem) SearchText() string { return c.DisplayName + " " + c.Description }
func (c *CommitActionItem) Value() interface{} { return c.ID }

// FileItem adapts file information for dropdown display
type FileItem struct {
	Filename    string
	Description string
}

func (f *FileItem) Display() string    { return f.Description }
func (f *FileItem) SearchText() string { return f.Filename + " " + f.Description }
func (f *FileItem) Value() interface{} { return f.Filename }

// Execute runs a simplified commit flow:
// 1) If there are staged changes, use them and generate a commit message
// 2) If there are no staged changes, prompt user to select files, then generate the commit message
func (cf *CommitFlow) Execute() error {
	// Interactive terminal
	if term.IsTerminal(int(os.Stdin.Fd())) {
		stagedFiles, _, err := cf.getGitStatus()
		if err != nil {
			return fmt.Errorf("failed to get git status: %w", err)
		}
		if len(stagedFiles) > 0 {
			return cf.generateCommitMessageAndCommit()
		}
		// No staged changes -> select files and then continue to message generation
		return cf.selectFilesToCommit()
	}

	// Non-interactive environments: just attempt to generate and print message
	return cf.executeNonInteractive()
}

// buildCommitActions creates dynamic actions based on git status
func (cf *CommitFlow) buildCommitActions(stagedFiles, unstagedFiles []string) []CommitAction {
	var actions []CommitAction

	// Option 1: Commit staged files (if any)
	if len(stagedFiles) > 0 {
		stagingInfo := fmt.Sprintf("Commit %d staged file(s)", len(stagedFiles))
		if len(stagedFiles) <= 3 {
			fileList := strings.Join(stagedFiles, ", ")
			stagingInfo += fmt.Sprintf(": %s", fileList)
		}

		actions = append(actions, CommitAction{
			ID:          "commit_staged",
			DisplayName: "📦 Commit Staged Files",
			Description: stagingInfo,
			Action:      (*CommitFlow).commitStagedFiles,
		})
	}

	// Option 2: Select specific files to stage and commit
	if len(unstagedFiles) > 0 {
		actions = append(actions, CommitAction{
			ID:          "select_files",
			DisplayName: "📝 Select Files to Commit",
			Description: fmt.Sprintf("Choose from %d modified file(s)", len(unstagedFiles)),
			Action:      (*CommitFlow).selectFilesToCommit,
		})
	}

	// Option 3: Stage all and commit (if there are unstaged changes)
	if len(unstagedFiles) > 0 {
		actions = append(actions, CommitAction{
			ID:          "commit_all",
			DisplayName: "🎯 Stage All & Commit",
			Description: fmt.Sprintf("Stage and commit all %d modified file(s)", len(unstagedFiles)),
			Action:      (*CommitFlow).stageAllAndCommit,
		})
	}

	// Option 4: Single file commit (if multiple files available)
	totalFiles := len(stagedFiles) + len(unstagedFiles)
	if totalFiles > 1 {
		actions = append(actions, CommitAction{
			ID:          "single_file",
			DisplayName: "📄 Single File Commit",
			Description: "Commit changes to just one file",
			Action:      (*CommitFlow).singleFileCommit,
		})
	}

	return actions
}

// getGitStatus returns lists of staged and unstaged files
func (cf *CommitFlow) getGitStatus() (staged, unstaged []string, err error) {
	// Get staged files
	stagedOutput, err := exec.Command("git", "diff", "--staged", "--name-only").CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get staged files: %w", err)
	}

	// Get unstaged files
	unstagedOutput, err := exec.Command("git", "diff", "--name-only").CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get unstaged files: %w", err)
	}

	// Parse staged files
	if len(stagedOutput) > 0 {
		for _, file := range strings.Split(strings.TrimSpace(string(stagedOutput)), "\n") {
			if strings.TrimSpace(file) != "" {
				staged = append(staged, file)
			}
		}
	}

	// Parse unstaged files
	if len(unstagedOutput) > 0 {
		for _, file := range strings.Split(strings.TrimSpace(string(unstagedOutput)), "\n") {
			if strings.TrimSpace(file) != "" {
				unstaged = append(unstaged, file)
			}
		}
	}

	return staged, unstaged, nil
}

// commitStagedFiles commits the currently staged files
func (cf *CommitFlow) commitStagedFiles() error {
	cf.println("")
	cf.println("📦 Committing staged files...")
	return cf.generateCommitMessageAndCommit()
}

// selectFilesToCommit allows the user to select specific files to commit
func (cf *CommitFlow) selectFilesToCommit() error {
	// Use console flow if in agent console
	if os.Getenv("LEDIT_AGENT_CONSOLE") == "1" {
		return cf.selectFilesToCommitConsole()
	}

	_, unstagedFiles, err := cf.getGitStatus()
	if err != nil {
		return err
	}

	if len(unstagedFiles) == 0 {
		cf.println("")
		cf.println("📭 No unstaged files to select.")
		return nil
	}

	// Simpler selection flow: use the console-style prompt from CommitCommand
	// to avoid dropdowns and complex decision trees. This will prompt the
	// user to enter file numbers or 'a' for all, then stage the chosen files.
	commitCmd := &CommitCommand{
		skipPrompt:   cf.skipPrompt,
		dryRun:       cf.dryRun,
		allowSecrets: cf.allowSecrets,
	}

	reader := bufio.NewReader(os.Stdin)
	selectedFiles, err := commitCmd.selectAndStageFiles(cf.agent, reader)
	if err != nil {
		return err
	}

	if len(selectedFiles) == 0 {
		cf.println("")
		cf.println("❌ No files selected")
		return nil
	}

	// Proceed to generate commit message and commit
	return cf.generateCommitMessageAndCommit()
}

// stageAllAndCommit stages all modified files and commits them
func (cf *CommitFlow) stageAllAndCommit() error {
	cf.println("")
	cf.println("🎯 Staging all modified files...")

	cmd := exec.Command("git", "add", "-A")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage files: %w", err)
	}

	cf.println("✅ All files staged.")
	return cf.generateCommitMessageAndCommit()
}

// singleFileCommit allows selecting and committing a single file
func (cf *CommitFlow) singleFileCommit() error {
	// Use console flow if in agent console
	if os.Getenv("LEDIT_AGENT_CONSOLE") == "1" {
		return cf.singleFileCommitConsole()
	}

	// Get all available files (staged + unstaged)
	stagedFiles, unstagedFiles, err := cf.getGitStatus()
	if err != nil {
		return err
	}

	allFiles := append(stagedFiles, unstagedFiles...)
	if len(allFiles) == 0 {
		cf.println("")
		cf.println("📭 No files available for commit.")
		return nil
	}

	// Convert to dropdown items with status indicators
	// UI not available - select first file or return error
	cf.println("Interactive file selection not available.")
	var selectedFile string
	if len(allFiles) > 0 {
		selectedFile = allFiles[0]
		cf.println(fmt.Sprintf("Auto-selected first file: %s", selectedFile))
	} else {
		return fmt.Errorf("no files available for selection")
	}

	// Reset staging area and stage only the selected file
	cf.println("")
	cf.println("🔄 Resetting staging area...")
	exec.Command("git", "reset").Run() // Reset staging area

	cf.printf("📤 Staging: %s\n", selectedFile)
	cmd := exec.Command("git", "add", selectedFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage file %s: %w", selectedFile, err)
	}

	return cf.generateCommitMessageAndCommit() // true = single file mode
}

// generateCommitMessageAndCommit handles the commit message generation and commit
func (cf *CommitFlow) generateCommitMessageAndCommit() error {
	// Get diff for commit message generation
	diffOutput, err := exec.Command("git", "diff", "--staged").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get staged diff: %w", err)
	}

	if len(diffOutput) == 0 {
		cf.println("")
		cf.println("📭 No staged changes to commit.")
		return nil
	}

	// Use existing commit message generation logic from the original commit command
	// This will use the diff optimizer we already implemented
	cf.println("")
	cf.println("🤖 Generating commit message...")

	// Create a temporary CommitCommand to reuse the existing logic
	commitCmd := &CommitCommand{
		skipPrompt:       cf.skipPrompt,
		dryRun:           cf.dryRun,
		allowSecrets:     cf.allowSecrets,
		userInstructions: cf.userInstructions,
	}
	return commitCmd.generateAndCommit(cf.agent, nil)
}

// executeNonInteractive handles non-interactive mode (fallback)
func (cf *CommitFlow) executeNonInteractive() error {
	// Fallback to original commit logic for non-terminal environments
	commitCmd := &CommitCommand{}
	return commitCmd.executeMultiFileCommit(cf.agent)
}
