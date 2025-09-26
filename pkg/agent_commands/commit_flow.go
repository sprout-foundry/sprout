package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"golang.org/x/term"
)

// CommitFlow manages the interactive commit workflow
type CommitFlow struct {
	agent     *agent.Agent
	optimizer *utils.DiffOptimizer
}

// NewCommitFlow creates a new commit flow
func NewCommitFlow(chatAgent *agent.Agent) *CommitFlow {
	return &CommitFlow{
		agent:     chatAgent,
		optimizer: utils.NewDiffOptimizer(),
	}
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

// Execute runs the interactive commit flow
func (cf *CommitFlow) Execute() error {
	// Check if we're in the agent console
	if os.Getenv("LEDIT_AGENT_CONSOLE") == "1" {
		return cf.executeConsoleFlow()
	}

	// Check for terminal support
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return cf.executeNonInteractive()
	}

	// Show commit flow options
	return cf.showCommitOptions()
}

// showCommitOptions displays the main commit flow options
func (cf *CommitFlow) showCommitOptions() error {
	// Check current git status first
	stagedFiles, unstagedFiles, err := cf.getGitStatus()
	if err != nil {
		return fmt.Errorf("failed to get git status: %w", err)
	}

	// Build dynamic options based on current state
	actions := cf.buildCommitActions(stagedFiles, unstagedFiles)

	if len(actions) == 0 {
		fmt.Printf("\r\n📭 No changes to commit. Working directory is clean.\r\n")
		return nil
	}

	// Convert to dropdown items
	items := make([]ui.DropdownItem, len(actions))
	for i, action := range actions {
		items[i] = &CommitActionItem{
			ID:          action.ID,
			DisplayName: action.DisplayName,
			Description: action.Description,
		}
	}

	// Create and show dropdown
	dropdown := ui.NewDropdown(items, ui.DropdownOptions{
		Prompt:       "🚀 Commit Workflow:",
		SearchPrompt: "Search: ",
		ShowCounts:   false,
	})

	// Temporarily disable ESC monitoring during dropdown
	cf.agent.DisableEscMonitoring()
	defer cf.agent.EnableEscMonitoring()

	selected, err := dropdown.Show()
	if err != nil {
		fmt.Printf("\r\nCommit workflow cancelled.\r\n")
		return nil
	}

	// Execute selected action
	selectedID := selected.Value().(string)
	for _, action := range actions {
		if action.ID == selectedID {
			return action.Action(cf)
		}
	}

	return fmt.Errorf("unknown action: %s", selectedID)
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
	fmt.Printf("\r\n📦 Committing staged files...\r\n")
	return cf.generateCommitMessageAndCommit(false) // false = not single file mode
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
		fmt.Printf("\r\n📭 No unstaged files to select.\r\n")
		return nil
	}

	// Convert files to dropdown items
	items := make([]ui.DropdownItem, len(unstagedFiles))
	for i, file := range unstagedFiles {
		items[i] = &FileItem{
			Filename:    file,
			Description: fmt.Sprintf("📝 Stage and commit: %s", file),
		}
	}

	// Create file selection dropdown
	dropdown := ui.NewDropdown(items, ui.DropdownOptions{
		Prompt:       "📝 Select File to Commit:",
		SearchPrompt: "Search files: ",
		ShowCounts:   true,
	})

	// Temporarily disable ESC monitoring during dropdown
	cf.agent.DisableEscMonitoring()
	defer cf.agent.EnableEscMonitoring()

	selected, err := dropdown.Show()
	if err != nil {
		fmt.Printf("\r\nFile selection cancelled.\r\n")
		return nil
	}

	selectedFile := selected.Value().(string)

	// Stage the selected file
	fmt.Printf("\r\n📤 Staging: %s\r\n", selectedFile)
	cmd := exec.Command("git", "add", selectedFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage file %s: %w", selectedFile, err)
	}

	// Determine if this is a single file commit
	singleFileMode := true
	return cf.generateCommitMessageAndCommit(singleFileMode)
}

// stageAllAndCommit stages all modified files and commits them
func (cf *CommitFlow) stageAllAndCommit() error {
	fmt.Printf("\r\n🎯 Staging all modified files...\r\n")

	cmd := exec.Command("git", "add", "-A")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage files: %w", err)
	}

	fmt.Printf("✅ All files staged.\r\n")
	return cf.generateCommitMessageAndCommit(false) // false = not single file mode
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
		fmt.Printf("\r\n📭 No files available for commit.\r\n")
		return nil
	}

	// Convert to dropdown items with status indicators
	items := make([]ui.DropdownItem, len(allFiles))
	for i, file := range allFiles {
		status := "📝 (unstaged)"
		for _, staged := range stagedFiles {
			if file == staged {
				status = "📦 (staged)"
				break
			}
		}
		items[i] = &FileItem{
			Filename:    file,
			Description: fmt.Sprintf("%s %s", status, file),
		}
	}

	// Create dropdown
	dropdown := ui.NewDropdown(items, ui.DropdownOptions{
		Prompt:       "📄 Select Single File to Commit:",
		SearchPrompt: "Search files: ",
		ShowCounts:   true,
	})

	// Show dropdown
	cf.agent.DisableEscMonitoring()
	defer cf.agent.EnableEscMonitoring()

	selected, err := dropdown.Show()
	if err != nil {
		fmt.Printf("\r\nSingle file commit cancelled.\r\n")
		return nil
	}

	selectedFile := selected.Value().(string)

	// Reset staging area and stage only the selected file
	fmt.Printf("\r\n🔄 Resetting staging area...\r\n")
	exec.Command("git", "reset").Run() // Reset staging area

	fmt.Printf("📤 Staging: %s\r\n", selectedFile)
	cmd := exec.Command("git", "add", selectedFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage file %s: %w", selectedFile, err)
	}

	return cf.generateCommitMessageAndCommit(true) // true = single file mode
}

// generateCommitMessageAndCommit handles the commit message generation and commit
func (cf *CommitFlow) generateCommitMessageAndCommit(singleFileMode bool) error {
	// Get diff for commit message generation
	diffOutput, err := exec.Command("git", "diff", "--staged").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get staged diff: %w", err)
	}

	if len(diffOutput) == 0 {
		fmt.Printf("\r\n📭 No staged changes to commit.\r\n")
		return nil
	}

	// Use existing commit message generation logic from the original commit command
	// This will use the diff optimizer we already implemented
	fmt.Printf("\r\n🤖 Generating commit message...\r\n")

	// Create a temporary CommitCommand to reuse the existing logic
	commitCmd := &CommitCommand{}
	return commitCmd.generateAndCommit(cf.agent, nil, singleFileMode)
}

// executeNonInteractive handles non-interactive mode (fallback)
func (cf *CommitFlow) executeNonInteractive() error {
	// Fallback to original commit logic for non-terminal environments
	commitCmd := &CommitCommand{}
	return commitCmd.executeMultiFileCommit(cf.agent)
}
