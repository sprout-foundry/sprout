package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/utils"
)

// CommitCommand implements the /commit slash command
type CommitCommand struct {
	skipPrompt   bool
	dryRun       bool
	allowSecrets bool
}

// wrapText wraps text to a specific line length
func wrapText(text string, lineLength int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var lines []string
	currentLine := words[0]

	for i := 1; i < len(words); i++ {
		word := words[i]
		if len(currentLine)+1+len(word) <= lineLength {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return strings.Join(lines, "\n")
}

// Name returns the command name
func (c *CommitCommand) Name() string {
	return "commit"
}

// Description returns the command description
func (c *CommitCommand) Description() string {
	return "Interactive commit workflow with dropdown selection - stage files and generate commit messages"
}

// Execute runs the commit command
func (c *CommitCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// Parse flags from args
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

	// Handle subcommands
	if len(cleanArgs) > 0 {
		switch cleanArgs[0] {
		case "single", "one", "file":
			return c.executeSingleFileCommit(cleanArgs[1:], chatAgent)
		case "help", "--help", "-h":
			return c.showHelp()
		default:
			return fmt.Errorf("unknown subcommand: %s. Use '/commit help' for usage", cleanArgs[0])
		}
	}

	// Default behavior: use new interactive commit flow
	flow := NewCommitFlow(chatAgent)
	return flow.Execute()
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

		if c.skipPrompt {
			fmt.Println("✅ Using staged files for commit (--skip-prompt)")
			// Skip to commit message generation - files are already staged
			return c.generateAndCommit(chatAgent, nil, false) // false = multi-file mode, nil = no reader needed
		}

		fmt.Println("\n💡 Use staged files for commit? (y/n, default: y):")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		if input == "" || input == "y" || input == "yes" {
			fmt.Println("✅ Using staged files for commit")
			// Skip to commit message generation - files are already staged
			return c.generateAndCommit(chatAgent, reader, false) // false = multi-file mode
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

	// Step 4: Select files (or auto-select all if skipPrompt)
	var filesToAdd []string
	var reader *bufio.Reader

	if c.skipPrompt {
		fmt.Println("\n✅ Auto-selecting all modified files (--skip-prompt)")
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
	} else {
		// Interactive file selection
		fmt.Println("\n💡 Enter file numbers to commit (comma-separated, 'a' for all, 'q' to quit):")
		reader = bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "q" || input == "quit" {
			fmt.Println("❌ Commit cancelled")
			return nil
		}

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

	return c.generateAndCommit(chatAgent, reader, false) // false = multi-file mode
}

// executeSingleFileCommit handles single file commit workflow
func (c *CommitCommand) executeSingleFileCommit(args []string, chatAgent *agent.Agent) error {
	fmt.Println("🚀 Starting single file commit workflow...")
	fmt.Println("=============================================")

	// Step 1: Check for already staged files
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

	// If exactly one file is staged, use it
	if len(validStagedFiles) == 1 {
		fmt.Printf("📦 Using staged file: %s\n", validStagedFiles[0])
		return c.generateAndCommit(chatAgent, nil, true) // true = single file mode
	}

	// If multiple files are staged, error
	if len(validStagedFiles) > 1 {
		fmt.Printf("❌ Error: %d files are already staged. Single file mode requires exactly one file.\n", len(validStagedFiles))
		fmt.Println("💡 Tip: Use '/commit' without 'single' for multi-file commits, or unstage files with 'git reset'")
		return nil
	}

	// Step 2: Show current git status
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

	// Step 4: Select ONE file (or auto-select first if skipPrompt)
	var index int
	var reader *bufio.Reader

	if c.skipPrompt {
		fmt.Println("\n✅ Auto-selecting first modified file (--skip-prompt)")
		index = 1 // Select first file
	} else {
		fmt.Println("\n💡 Enter file number to commit (only one file allowed in single mode, 'q' to quit):")
		reader = bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "q" || input == "quit" {
			fmt.Println("❌ Commit cancelled")
			return nil
		}

		// Parse the single file selection
		_, err = fmt.Sscanf(input, "%d", &index)
		if err != nil || index < 1 || index > len(validStatusLines) {
			fmt.Printf("❌ Invalid selection: %s\n", input)
			return nil
		}
	}

	line := validStatusLines[index-1]
	parts := strings.Fields(line)
	if len(parts) < 2 {
		fmt.Println("❌ Could not parse file from status")
		return nil
	}

	filename := strings.Join(parts[1:], " ")
	fmt.Printf("✅ Selected: %s\n", filename)

	// Step 5: Stage the file
	fmt.Println("\n📦 Staging file...")
	cmd := exec.Command("git", "add", filename)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ Failed to stage %s: %v\n", filename, err)
		fmt.Printf("Output: %s\n", string(output))
		return nil
	}
	fmt.Printf("✅ Staged: %s\n", filename)

	return c.generateAndCommit(chatAgent, reader, true) // true = single file mode
}

// showHelp displays commit command usage
func (c *CommitCommand) showHelp() error {
	fmt.Println("📝 Commit Command Usage:")
	fmt.Println("========================")
	fmt.Println("/commit          - Interactive commit workflow with dropdown selection")
	fmt.Println("/commit single   - Single file commit workflow (strict format)")
	fmt.Println("/commit help     - Show this help message")
	fmt.Println()
	fmt.Println("The interactive workflow offers:")
	fmt.Println("🚀 Smart commit options based on your current git status")
	fmt.Println("📦 Commit staged files")
	fmt.Println("📝 Select specific files to stage and commit")
	fmt.Println("🎯 Stage all modified files and commit")
	fmt.Println("📄 Single file commit mode")
	fmt.Println()
	fmt.Println("Features:")
	fmt.Println("✨ Optimized diff processing for large files")
	fmt.Println("🤖 AI-generated commit messages")
	fmt.Println("🔍 Smart file detection and summaries")
	fmt.Println()
	fmt.Println("Single file commits follow strict formatting:")
	fmt.Println("- Lowercase verb start (add, fix, update, etc.)")
	fmt.Println("- 50 character title limit")
	fmt.Println("- Focus on what changed, not filename")
	return nil
}

// generateAndCommit handles commit message generation and commit creation
func (c *CommitCommand) generateAndCommit(chatAgent *agent.Agent, reader *bufio.Reader, singleFileMode bool) error {
	// If reader is nil, create one
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}

	// Generate commit message
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

	// Use the current agent's configured provider and model for commit generation
	configManager := chatAgent.GetConfigManager()
	clientType, err := configManager.GetProvider()
	if err != nil {
		return fmt.Errorf("failed to get provider: %v", err)
	}
	model := configManager.GetModelForProvider(clientType)

	client, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		return fmt.Errorf("failed to create client: %v", err)
	}

	// Get current branch name
	branchOutput, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get branch name: %v", err)
	}
	branch := strings.TrimSpace(string(branchOutput))

	// Check if it's a default branch
	defaultBranches := []string{"master", "main", "develop", "dev"}
	isDefaultBranch := false
	for _, db := range defaultBranches {
		if branch == db {
			isDefaultBranch = true
			break
		}
	}

	// Get staged files with their status
	stagedFilesOutput, err := exec.Command("git", "diff", "--cached", "--name-status").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get staged files status: %v", err)
	}

	// Parse file actions
	fileActions := []string{}
	primaryAction := ""
	lines := strings.Split(strings.TrimSpace(string(stagedFilesOutput)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			status := parts[0]
			filepath := strings.Join(parts[1:], " ")

			action := ""
			switch status {
			case "A":
				action = "Adds"
			case "D":
				action = "Deletes"
			case "R":
				action = "Renames"
			default:
				action = "Updates"
			}

			if primaryAction == "" {
				primaryAction = action
			}

			fileActions = append(fileActions, fmt.Sprintf("%s %s", action, filepath))
		}
	}

	// Build the file actions summary (detailed for single file, generic for multi-file)
	var fileActionsSummary string
	if len(fileActions) == 1 {
		// Single file: include the specific action
		fileActionsSummary = fileActions[0]
	} else {
		// Multi-file: use generic summary
		fileActionsSummary = fmt.Sprintf("%s %d files", primaryAction, len(fileActions))
	}

	// Build branch prefix if not on default branch
	branchPrefix := ""
	if !isDefaultBranch {
		branchPrefix = fmt.Sprintf("[%s] ", branch)
	}

	var commitMessage string

	// Retry loop for commit message generation
	for {
		if singleFileMode {
			// Single file mode - simple format without file actions in title
			// Optimize diff for API efficiency
			optimizer := utils.NewDiffOptimizer()
			optimizedDiff := optimizer.OptimizeDiff(string(diffOutput))

			// Build context info for large files
			var contextInfo string
			if len(optimizedDiff.FileSummaries) > 0 {
				contextInfo = "\n\nFile summaries for optimized content:\n"
				for file, summary := range optimizedDiff.FileSummaries {
					contextInfo += fmt.Sprintf("- %s: %s\n", file, summary)
				}
			}

			// Create the commit message generation prompt
			commitPrompt := fmt.Sprintf(`Generate a concise git commit message for the following staged changes.

STRICT RULES FOR SINGLE FILE COMMITS:
1. Title MUST start with a lowercase action verb (add, update, fix, remove, refactor, etc.)
2. Title must be under 50 characters
3. Title should be specific about WHAT changed, not just the filename
4. No colons, no markdown, no punctuation at the end
5. Then a blank line
6. Then a description paragraph under 300 characters
7. Description should explain WHY the change was made
8. No code blocks, no markdown formatting
9. Format: [title]\n\n[description]

Examples:
- "add user authentication middleware" (not "add auth.js")
- "fix memory leak in image processing" (not "fix bug in processor.go")
- "update database connection timeout" (not "update config")

Staged diff:
%s%s

Generate only the commit message, no additional commentary.`, optimizedDiff.OptimizedContent, contextInfo)

			// Get commit message using fast model
			messages := []api.Message{
				{
					Role:    "system",
					Content: "You are a git commit message generator. Generate concise, clear commit messages following conventional commit standards with strict single file rules.",
				},
				{
					Role:    "user",
					Content: commitPrompt,
				},
			}

			resp, err := client.SendChatRequest(messages, nil, "")
			if err != nil {
				return fmt.Errorf("failed to generate commit message: %v", err)
			}

			if len(resp.Choices) == 0 {
				return fmt.Errorf("no response from model")
			}

			commitMessage = strings.TrimSpace(resp.Choices[0].Message.Content)

			// Validate commit message format for single file commits
			lines := strings.Split(commitMessage, "\n")
			if len(lines) > 0 {
				title := lines[0]
				// Check if title starts with lowercase
				if len(title) > 0 && strings.ToUpper(title[:1]) == title[:1] {
					// Auto-fix: convert first letter to lowercase
					title = strings.ToLower(title[:1]) + title[1:]
					lines[0] = title
					commitMessage = strings.Join(lines, "\n")
				}
			}

			// Show token usage
			fmt.Printf("\n💰 Tokens used: %d (model: %s/%s)\n", resp.Usage.TotalTokens, clientType, model)

		} else {
			// Multi-file mode - full format with file actions
			// Calculate available space for title
			prefixAndActions := branchPrefix + fileActionsSummary + " - "
			availableSpace := 72 - len(prefixAndActions)

			// Optimize diff for API efficiency
			optimizer := utils.NewDiffOptimizer()
			optimizedDiff := optimizer.OptimizeDiff(string(diffOutput))

			// Build context info for large files
			var contextInfo string
			if len(optimizedDiff.FileSummaries) > 0 {
				contextInfo = "\n\nFile summaries for optimized content:\n"
				for file, summary := range optimizedDiff.FileSummaries {
					contextInfo += fmt.Sprintf("- %s: %s\n", file, summary)
				}
			}

			// Create the commit message generation prompt
			commitPrompt := fmt.Sprintf(`Base responses on the following changes:

%s%s

Generate a concise git commit title starting with the word: '%s'. 
The total length MUST be under %d characters. Don't include the file name or any 
colons. The title should be a single line without any markdown formatting. Only 
return the short title and nothing else.`, optimizedDiff.OptimizedContent, contextInfo, primaryAction, availableSpace)

			// Get commit message title using fast model
			messages := []api.Message{
				{
					Role:    "system",
					Content: "You are a git commit message generator. Generate concise, clear commit messages following conventional commit standards.",
				},
				{
					Role:    "user",
					Content: commitPrompt,
				},
			}

			resp, err := client.SendChatRequest(messages, nil, "")
			if err != nil {
				return fmt.Errorf("failed to generate commit message: %v", err)
			}

			if len(resp.Choices) == 0 {
				return fmt.Errorf("no response from model")
			}

			shortTitle := strings.TrimSpace(resp.Choices[0].Message.Content)

			// Now generate the description (reuse the optimized diff)
			descPrompt := fmt.Sprintf(`Base responses on the following changes:

%s%s

Generate a Git commit message summary. The message should follow these rules:
1. The total length MUST be under 500 characters.
2. DO NOT include a title.
3. DO NOT include any code blocks or filenames.
4. DO NOT include any user messages.
5. Message will be a single paragraph without any markdown formatting.
6. The message should be clear and concise and only give reasoning for the change 
   if provided by the user.`, optimizedDiff.OptimizedContent, contextInfo)

			// Get description
			messages = []api.Message{
				{
					Role:    "system",
					Content: "You are a git commit message generator. Generate clear, concise descriptions.",
				},
				{
					Role:    "user",
					Content: descPrompt,
				},
			}

			resp, err = client.SendChatRequest(messages, nil, "")
			if err != nil {
				return fmt.Errorf("failed to generate description: %v", err)
			}

			if len(resp.Choices) == 0 {
				return fmt.Errorf("no response from model for description")
			}

			description := strings.TrimSpace(resp.Choices[0].Message.Content)

			// Wrap description at 72 characters
			wrappedDesc := wrapText(description, 72)

			// Build the full commit message
			commitTitle := branchPrefix + fileActionsSummary + " - " + shortTitle
			commitMessage = commitTitle + "\n\n" + wrappedDesc

			// Show token usage (both requests)
			fmt.Printf("\n💰 Tokens used: ~%d (model: %s/%s)\n", resp.Usage.TotalTokens*2, clientType, model)
		}

		// Show commit message preview
		fmt.Println("\n📋 Commit message preview:")
		fmt.Println("=============================================")
		fmt.Println(commitMessage)
		fmt.Println("=============================================")

		// Handle confirmation (or auto-proceed if skipPrompt)
		if c.skipPrompt {
			fmt.Println("\n✅ Auto-proceeding with commit (--skip-prompt)")
			break // Exit retry loop
		} else {
			// Confirmation with retry option
			fmt.Print("\n💡 Proceed with commit? (y/n/e to edit/r to retry): ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))

			if input == "r" || input == "retry" {
				fmt.Println("🔄 Regenerating commit message...")
				continue // Go back to start of loop to regenerate
			} else if input == "e" || input == "edit" {
				// Allow editing the commit message
				fmt.Println("\n✏️  Enter your commit message (press Enter twice when done):")
				var editedMessage strings.Builder
				emptyLineCount := 0
				for {
					line, _ := reader.ReadString('\n')
					if line == "\n" {
						emptyLineCount++
						if emptyLineCount >= 2 {
							break
						}
					} else {
						emptyLineCount = 0
					}
					editedMessage.WriteString(line)
				}
				commitMessage = strings.TrimSpace(editedMessage.String())
				break // Exit retry loop with edited message
			} else if input == "y" || input == "yes" || input == "" {
				break // Exit retry loop and proceed with commit
			} else if input == "n" || input == "no" {
				fmt.Println("❌ Commit cancelled")
				return nil
			} else {
				fmt.Printf("❌ Invalid option: %s. Please use y/n/e/r\n", input)
				continue // Show the confirmation prompt again
			}
		}

	} // End of retry loop

	// Handle dry-run mode
	if c.dryRun {
		fmt.Println("\n🔍 Dry-run mode: Commit message generated successfully!")
		fmt.Println("💡 The commit was not created due to --dry-run flag")
		fmt.Println("📝 To create the commit, run the command again without --dry-run")
		return nil
	}

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
		return fmt.Errorf("failed to create commit: %v\nOutput: %s", err, string(output))
	}

	fmt.Printf("✅ Commit created successfully!\n")
	fmt.Printf("Output: %s\n", string(output))

	return nil
}
