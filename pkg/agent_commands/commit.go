package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
)

// CommitCommand implements the /commit slash command
type CommitCommand struct{}

// Name returns the command name
func (c *CommitCommand) Name() string {
	return "commit"
}

// Description returns the command description
func (c *CommitCommand) Description() string {
	return "Interactive commit workflow - select files and generate commit messages"
}

// Execute runs the commit command
func (c *CommitCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// Handle subcommands
	if len(args) > 0 {
		switch args[0] {
		case "single", "one", "file":
			return c.executeSingleFileCommit(args[1:], chatAgent)
		case "help", "--help", "-h":
			return c.showHelp()
		default:
			return fmt.Errorf("unknown subcommand: %s. Use '/commit help' for usage", args[0])
		}
	}

	// Default behavior: multi-file commit
	return c.executeMultiFileCommit(chatAgent)
}

// executeMultiFileCommit handles the original multi-file commit workflow
func (c *CommitCommand) executeMultiFileCommit(chatAgent *agent.Agent) error {
	fmt.Println("🚀 Starting interactive commit workflow...")
	fmt.Println("=============================================")

	// Step 1: Check for staged files first
	stagedOutput, err := exec.Command("git", "diff", "--staged", "--name-only").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get staged files: %v", err)
	}

	stagedFiles := strings.Split(strings.TrimSpace(string(stagedOutput)), "\n")
	var validStagedFiles []string
	for _, file := range stagedFiles {
		if strings.TrimSpace(file) != "" {
			validStagedFiles = append(validStagedFiles, file)
		}
	}

	// If we have staged files, use them by default
	if len(validStagedFiles) > 0 {
		fmt.Printf("📦 Found %d staged file(s):\n", len(validStagedFiles))
		for i, file := range validStagedFiles {
			fmt.Printf("%2d. %s\n", i+1, file)
		}
		
		fmt.Println("\n💡 Use staged files for commit? (y/n, default: y):")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		
		if input == "" || input == "y" || input == "yes" {
			fmt.Println("✅ Using staged files for commit")
			// Skip to commit message generation - files are already staged
			return c.generateAndCommit(chatAgent, reader)
		}
		
		if input == "n" || input == "no" {
			fmt.Println("🔄 Proceeding to file selection...")
			// Fall through to file selection workflow
		} else {
			fmt.Println("❌ Invalid option")
			return nil
		}
	}

	// Step 2: Show current git status for file selection
	fmt.Println("📊 Current git status:")
	statusOutput, err := exec.Command("git", "status", "--porcelain").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get git status: %v", err)
	}

	if len(statusOutput) == 0 {
		fmt.Println("✅ No changes to commit")
		return nil
	}

	statusLines := strings.Split(strings.TrimSpace(string(statusOutput)), "\n")

	// Filter out empty lines to match single file commit behavior
	var validStatusLines []string
	for _, line := range statusLines {
		if strings.TrimSpace(line) != "" {
			validStatusLines = append(validStatusLines, line)
		}
	}

	if len(validStatusLines) == 0 {
		fmt.Println("✅ No changes to commit")
		return nil
	}

	// Step 3: Show available files
	fmt.Println("\n📁 Modified files:")
	for i, line := range validStatusLines {
		fmt.Printf("%2d. %s\n", i+1, line)
	}

	// Step 4: Prompt user to select files
	fmt.Println("\n💡 Enter file numbers to commit (comma-separated, 'a' for all, 'q' to quit):")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "q" || input == "quit" {
		fmt.Println("❌ Commit cancelled")
		return nil
	}

	var filesToAdd []string

	if input == "a" || input == "all" {
		// Add all modified files
		for _, line := range validStatusLines {
			// Split on spaces and take everything after the status field
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// Join all parts except the first (status) to handle filenames with spaces
				filename := strings.Join(parts[1:], " ")
				filesToAdd = append(filesToAdd, filename)
			}
		}
		fmt.Println("✅ Adding all modified files")
	} else {
		// Parse selected file numbers
		selections := strings.Split(input, ",")
		for _, sel := range selections {
			sel = strings.TrimSpace(sel)
			if sel == "" {
				continue
			}

			var index int
			_, err := fmt.Sscanf(sel, "%d", &index)
			if err != nil || index < 1 || index > len(validStatusLines) {
				fmt.Printf("❌ Invalid selection: %s\n", sel)
				continue
			}

			line := validStatusLines[index-1]
			// Split on spaces and take everything after the status field
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// Join all parts except the first (status) to handle filenames with spaces
				filename := strings.Join(parts[1:], " ")
				filesToAdd = append(filesToAdd, filename)
				fmt.Printf("✅ Adding: %s\n", filename)
			}
		}
	}

	if len(filesToAdd) == 0 {
		fmt.Println("❌ No files selected")
		return nil
	}

	// Step 5: Stage the selected files
	fmt.Println("\n📦 Staging files...")
	for _, file := range filesToAdd {
		cmd := exec.Command("git", "add", file)
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to stage %s: %v\n", file, err)
			fmt.Printf("Output: %s\n", string(output))
		} else {
			fmt.Printf("✅ Staged: %s\n", file)
		}
	}

	return c.generateAndCommit(chatAgent, reader)
}

// generateAndCommit handles commit message generation and commit creation
func (c *CommitCommand) generateAndCommit(chatAgent *agent.Agent, reader *bufio.Reader) error {
	// Generate commit message from staged diff
	fmt.Println("\n📝 Generating commit message...")

	// Get staged diff
	diffOutput, err := exec.Command("git", "diff", "--staged").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get staged diff: %v", err)
	}

	if len(strings.TrimSpace(string(diffOutput))) == 0 {
		fmt.Println("❌ No changes staged")
		return nil
	}

	// Use the agent to generate a commit message following gmitllm rules
	commitPrompt := fmt.Sprintf(`Generate a concise git commit message for the following staged changes.

IMPORTANT: Do NOT use any tools. Rely SOLELY on the staged diff provided below.

Follow these exact rules:
1. First, generate a short title starting with an action word (Adds, Updates, Deletes, Renames)
2. Title must be under 72 characters, no colons, no markdown
3. Title should not include filenames
4. Then generate a description paragraph under 500 characters
5. Description should not include code blocks or filenames
6. No markdown formatting anywhere
7. Format: [Title]\n\n[Description]

Staged changes:
%s

Please generate only the commit message content, no additional commentary.`, string(diffOutput))

	fmt.Println("🤖 Generating commit message with AI...")
	commitMessage, err := chatAgent.ProcessQuery(commitPrompt)
	if err != nil {
		return fmt.Errorf("failed to generate commit message: %v", err)
	}

	// Clean up the commit message
	commitMessage = strings.TrimSpace(commitMessage)

	// Apply text wrapping to description for multi-file commits
	lines := strings.Split(commitMessage, "\n")
	if len(lines) > 2 {
		description := strings.Join(lines[2:], "\n")
		wrappedDescription := wrapText(description, 72)
		commitMessage = lines[0] + "\n\n" + wrappedDescription
	}

	// Validate the commit message format
	if err := validateCommitFormat(commitMessage, false); err != nil {
		fmt.Printf("⚠️ Generated message format issue: %v\n", err)
		// Continue anyway - the user can edit if needed
	}

	// Use the commit utility to handle confirmation, editing, and retry
	finalCommitMessage, shouldCommit, err := handleCommitConfirmation(commitMessage, chatAgent, reader, diffOutput, "")
	if err != nil {
		return fmt.Errorf("commit confirmation failed: %v", err)
	}

	if !shouldCommit {
		fmt.Println("❌ Commit cancelled")
		return nil
	}

	commitMessage = finalCommitMessage

	// Create the commit
	fmt.Println("\n💾 Creating commit...")

	// Write commit message to temporary file
	tempFile := "commit_msg.txt"
	err = os.WriteFile(tempFile, []byte(commitMessage), 0644)
	if err != nil {
		return fmt.Errorf("failed to create temporary commit message file: %v", err)
	}
	defer os.Remove(tempFile)

	cmd := exec.Command("git", "commit", "-F", tempFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create commit: %v", err)
	}

	fmt.Printf("✅ Commit created successfully!\n")
	fmt.Printf("Output: %s\n", string(output))

	return nil
}
