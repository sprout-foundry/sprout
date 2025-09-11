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

// executeSingleFileCommit handles single file commit workflow
func (c *CommitCommand) executeSingleFileCommit(args []string, chatAgent *agent.Agent) error {
	fmt.Println("🚀 Starting single file commit workflow...")
	fmt.Println("=============================================")

	// Step 1: Show current git status
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

	// Filter out empty lines
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

	// Step 2: Show available files
	fmt.Println("\n📁 Modified files:")
	for i, line := range validStatusLines {
		fmt.Printf("%2d. %s\n", i+1, line)
	}

	// Step 3: Prompt user to select a single file
	fmt.Println("\n💡 Enter file number to commit (1-%d, 'q' to quit):", len(validStatusLines))
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "q" || input == "quit" {
		fmt.Println("❌ Commit cancelled")
		return nil
	}

	// Parse single file selection
	var index int
	_, err = fmt.Sscanf(input, "%d", &index)
	if err != nil || index < 1 || index > len(validStatusLines) {
		return fmt.Errorf("invalid selection. Please enter a number between 1 and %d", len(validStatusLines))
	}

	// Extract filename from git status line
	line := validStatusLines[index-1]
	// Split on spaces and take everything after the status field
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return fmt.Errorf("invalid git status line format: %s", line)
	}

	// Join all parts except the first (status) to handle filenames with spaces
	fileToAdd := strings.Join(parts[1:], " ")
	fmt.Printf("✅ Selected: %s\n", fileToAdd)

	// Step 4: Stage the selected file
	fmt.Println("\n📦 Staging file...")
	cmd := exec.Command("git", "add", fileToAdd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ Failed to stage %s: %v\n", fileToAdd, err)
		fmt.Printf("Output: %s\n", string(output))
		return fmt.Errorf("failed to stage file")
	}
	fmt.Printf("✅ Staged: %s\n", fileToAdd)

	// Step 5: Generate commit message from staged diff
	fmt.Println("\n📝 Generating commit message...")

	// Get staged diff for just this file
	diffOutput, err := exec.Command("git", "diff", "--staged", "--", fileToAdd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get staged diff: %v", err)
	}

	if len(strings.TrimSpace(string(diffOutput))) == 0 {
		fmt.Println("❌ No changes staged for file")
		return nil
	}

	// Get branch prefix and file status for detailed format
	branchPrefix := getBranchPrefix()
	status, _ := parseGitStatus(fmt.Sprintf("M %s", fileToAdd)) // Assume modified for now
	action := getFileAction(status)

	// Use the agent to generate a commit message with detailed format
	commitPrompt := fmt.Sprintf(`Generate a git commit message following this EXACT format:

%s%s %s - [Short Title]

[Detailed description]

IMPORTANT: Do NOT use any tools. Rely SOLELY on the staged diff provided below.

Format Requirements:
1. Start with branch prefix (already provided): "%s"
2. File action (already provided): "%s %s"  
3. Title separator: " - "
4. Short title: ≤72 characters total (including prefix and file action)
5. Blank line after title
6. Description: ≤500 characters, wrapped to 72 characters per line
7. No code blocks, no file names in description, no markdown

Generate ONLY the commit message in the exact format shown above.

Staged changes:
%s`, branchPrefix, action, fileToAdd, branchPrefix, action, fileToAdd, string(diffOutput))

	fmt.Println("🤖 Generating commit message with AI...")
	commitMessage, err := chatAgent.ProcessQuery(commitPrompt)
	if err != nil {
		return fmt.Errorf("failed to generate commit message: %v", err)
	}

	// Clean up the commit message
	commitMessage = strings.TrimSpace(commitMessage)

	// Apply text wrapping to description for single-file commits
	lines := strings.Split(commitMessage, "\n")
	if len(lines) > 2 {
		description := strings.Join(lines[2:], "\n")
		wrappedDescription := wrapText(description, 72)
		commitMessage = lines[0] + "\n\n" + wrappedDescription
	}

	// Validate the commit message format
	if err := validateCommitFormat(commitMessage, true); err != nil {
		fmt.Printf("⚠️ Generated message format issue: %v\n", err)
		// Continue anyway - the user can edit if needed
	}

	// Use the commit utility to handle confirmation, editing, and retry
	finalCommitMessage, shouldCommit, err := handleCommitConfirmation(commitMessage, chatAgent, reader, diffOutput, fileToAdd)
	if err != nil {
		return fmt.Errorf("commit confirmation failed: %v", err)
	}

	if !shouldCommit {
		fmt.Println("❌ Commit cancelled")
		return nil
	}

	commitMessage = finalCommitMessage

	// Step 7: Create the commit
	fmt.Println("\n💾 Creating commit...")

	// Write commit message to temporary file
	tempFile := "commit_msg.txt"
	err = os.WriteFile(tempFile, []byte(commitMessage), 0644)
	if err != nil {
		return fmt.Errorf("failed to create temporary commit message file: %v", err)
	}
	defer os.Remove(tempFile)

	cmd = exec.Command("git", "commit", "-F", tempFile)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create commit: %v", err)
	}

	fmt.Printf("✅ Commit created successfully for %s!\n", fileToAdd)
	fmt.Printf("Output: %s\n", string(output))

	return nil
}

// showHelp displays commit command usage
func (c *CommitCommand) showHelp() error {
	fmt.Println(`
📝 Commit Command Usage:
========================

/commit          - Interactive multi-file commit workflow
/commit single   - Single file commit workflow
/commit one      - Single file commit workflow (alias)
/commit file     - Single file commit workflow (alias)
/commit help     - Show this help message

Single file workflow:
- Shows modified files
- Allows selecting exactly one file
- Generates commit message focused on that specific file
- Commits only the selected file

Multi-file workflow:
- First checks for already staged files and offers to use them (default: yes)
- If no staged files or user declines, shows modified files for selection
- Allows selecting multiple files (comma-separated or 'all')
- Generates commit message for all staged changes
- Commits all staged files together
`)
	return nil
}

// handleCommitConfirmation handles the commit message confirmation, editing, and retry logic
func handleCommitConfirmation(commitMessage string, chatAgent *agent.Agent, reader *bufio.Reader, diffOutput []byte, contextInfo string) (string, bool, error) {
	maxRetries := 3
	retryCount := 0

	for {
		// Show preview
		fmt.Println("\n📋 Commit message preview:")
		fmt.Println("=============================================")
		fmt.Println(commitMessage)
		fmt.Println("=============================================")

		// Prompt for action
		fmt.Println("\n💡 Options:")
		fmt.Println("  y - Commit with this message")
		fmt.Println("  n - Cancel commit")
		fmt.Println("  e - Edit message in default editor")
		if retryCount < maxRetries {
			fmt.Println("  r - Retry AI generation")
		}
		fmt.Print("Choose an option: ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "y", "yes":
			return commitMessage, true, nil

		case "n", "no":
			return "", false, nil

		case "e", "edit":
			// Edit via default editor
			editedMessage, err := editCommitMessageInEditor(commitMessage)
			if err != nil {
				fmt.Printf("❌ Failed to edit message: %v\n", err)
				continue
			}
			commitMessage = editedMessage
			fmt.Println("✅ Message edited successfully")

		case "r", "retry":
			if retryCount >= maxRetries {
				fmt.Println("❌ Maximum retry attempts reached")
				continue
			}

			// Retry AI generation
			retryCount++
			fmt.Printf("🔄 Retrying AI generation (attempt %d/%d)...\n", retryCount, maxRetries)

			var retryPrompt string
			if contextInfo != "" {
				// Single file context - use detailed format
				branchPrefix := getBranchPrefix()
				status, _ := parseGitStatus(fmt.Sprintf("M %s", contextInfo)) // Assume modified for retry
				action := getFileAction(status)

				retryPrompt = fmt.Sprintf(`Generate a git commit message following this EXACT format:

%s%s %s - [Short Title]

[Detailed description]

IMPORTANT: Do NOT use any tools. Rely SOLELY on the staged diff provided below.

Format Requirements:
1. Start with branch prefix (already provided): "%s"
2. File action (already provided): "%s %s"  
3. Title separator: " - "
4. Short title: ≤72 characters total (including prefix and file action)
5. Blank line after title
6. Description: ≤500 characters, wrapped to 72 characters per line
7. No code blocks, no file names in description, no markdown

Generate ONLY the commit message in the exact format shown above.

Staged changes:
%s`, branchPrefix, action, contextInfo, branchPrefix, action, contextInfo, string(diffOutput))
			} else {
				// Multi-file context
				retryPrompt = fmt.Sprintf(`Generate a concise git commit message for the following staged changes.

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
			}

			newMessage, err := chatAgent.ProcessQuery(retryPrompt)
			if err != nil {
				fmt.Printf("❌ Failed to regenerate commit message: %v\n", err)
				continue
			}
			commitMessage = strings.TrimSpace(newMessage)

			// Apply text wrapping and validation for retry
			lines := strings.Split(commitMessage, "\n")
			if len(lines) > 2 {
				description := strings.Join(lines[2:], "\n")
				wrappedDescription := wrapText(description, 72)
				commitMessage = lines[0] + "\n\n" + wrappedDescription
			}

			// Validate the regenerated message
			isSingleFile := contextInfo != ""
			if err := validateCommitFormat(commitMessage, isSingleFile); err != nil {
				fmt.Printf("⚠️ Regenerated message format issue: %v\n", err)
				// Continue anyway - the user can edit if needed
			}

			fmt.Println("✅ Message regenerated successfully")

		default:
			fmt.Println("❌ Invalid option. Please choose y, n, e, or r")
		}
	}
}

// editCommitMessageInEditor opens the commit message in the user's default editor
func editCommitMessageInEditor(initialMessage string) (string, error) {
	// Create temporary file
	tempFile := ".commit_msg_edit.txt"
	err := os.WriteFile(tempFile, []byte(initialMessage), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer os.Remove(tempFile)

	// Determine editor (use $EDITOR or fallback to vim/nano)
	editor := os.Getenv("EDITOR")
	if editor == "" {
		// Try common editors
		if _, err := exec.LookPath("vim"); err == nil {
			editor = "vim"
		} else if _, err := exec.LookPath("nano"); err == nil {
			editor = "nano"
		} else if _, err := exec.LookPath("code"); err == nil {
			editor = "code"
		} else {
			return "", fmt.Errorf("no editor found. Please set $EDITOR environment variable")
		}
	}

	// Open editor
	cmd := exec.Command(editor, tempFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("editor failed: %v", err)
	}

	// Read edited content
	editedContent, err := os.ReadFile(tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %v", err)
	}

	return strings.TrimSpace(string(editedContent)), nil
}

// getBranchPrefix returns the branch prefix for commit messages
func getBranchPrefix() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "" // No prefix if we can't get branch
	}

	branchName := strings.TrimSpace(string(output))
	defaultBranches := []string{"master", "main", "develop", "dev"}

	// Don't add prefix for default branches
	for _, defaultBranch := range defaultBranches {
		if branchName == defaultBranch {
			return ""
		}
	}

	return fmt.Sprintf("[%s] ", branchName)
}

// getFileAction maps git status to action verbs
func getFileAction(status string) string {
	switch status {
	case "A":
		return "Adds"
	case "D":
		return "Deletes"
	case "R":
		return "Renames"
	default:
		return "Updates"
	}
}

// parseGitStatus extracts file status and path from git status line
func parseGitStatus(statusLine string) (status, filePath string) {
	if len(statusLine) < 3 {
		return "", ""
	}

	// Git status format: XY filename
	// We care about the staged status (first character)
	status = string(statusLine[0])
	filePath = strings.TrimSpace(statusLine[3:])

	return status, filePath
}

// wrapText wraps text to specified line length
func wrapText(text string, lineLength int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	var lines []string
	currentLine := ""

	for _, word := range words {
		// If adding this word would exceed line length, start a new line
		if len(currentLine)+len(word)+1 > lineLength && currentLine != "" {
			lines = append(lines, currentLine)
			currentLine = word
		} else if currentLine == "" {
			currentLine = word
		} else {
			currentLine += " " + word
		}
	}

	// Add the last line
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return strings.Join(lines, "\n")
}

// validateCommitFormat validates commit message format
func validateCommitFormat(message string, isSingleFile bool) error {
	lines := strings.Split(message, "\n")
	if len(lines) == 0 {
		return fmt.Errorf("commit message cannot be empty")
	}

	titleLine := lines[0]

	if isSingleFile {
		// Single file: detailed format validation
		if len(titleLine) > 72 {
			return fmt.Errorf("single-file commit title exceeds 72 characters (%d)", len(titleLine))
		}

		// Check for required format: [prefix] Action file - Title
		if !strings.Contains(titleLine, " - ") {
			return fmt.Errorf("single-file commit title missing ' - ' separator")
		}

		// Check description length if present
		if len(lines) > 2 {
			description := strings.Join(lines[2:], "\n")
			if len(description) > 500 {
				return fmt.Errorf("single-file commit description exceeds 500 characters (%d)", len(description))
			}
		}
	} else {
		// Multi-file: simple format validation
		if len(titleLine) > 72 {
			return fmt.Errorf("multi-file commit title exceeds 72 characters (%d)", len(titleLine))
		}

		if len(lines) > 2 {
			description := strings.Join(lines[2:], "\n")
			if len(description) > 500 {
				return fmt.Errorf("multi-file commit description exceeds 500 characters (%d)", len(description))
			}
		}
	}

	return nil
}
