package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
)

// CommitMessageHandler handles commit message generation, editing, and retry logic
type CommitMessageHandler struct {
	chatAgent *agent.Agent
	reader    *bufio.Reader
}

// NewCommitMessageHandler creates a new commit message handler
func NewCommitMessageHandler(chatAgent *agent.Agent, reader *bufio.Reader) *CommitMessageHandler {
	return &CommitMessageHandler{
		chatAgent: chatAgent,
		reader:    reader,
	}
}

// GenerateCommitMessage generates a commit message from staged diff
func (h *CommitMessageHandler) GenerateCommitMessage(diffOutput []byte, isSingleFile bool, filename string) (string, error) {
	var commitPrompt string

	if isSingleFile {
		commitPrompt = fmt.Sprintf(`Generate a concise commit message for changes to the file "%s".

IMPORTANT: Do NOT use any tools. Rely SOLELY on the staged diff provided below.

Requirements:
- Title: Maximum 120 characters, descriptive and concise
- Blank line after title
- Summary: Brief description of changes (be concise)
- Focus on what changed in this specific file and why, not how
- Include the filename in the summary if appropriate

Staged changes for %s:
%s

Please generate only the commit message content, no additional commentary.`, filename, filename, string(diffOutput))
	} else {
		commitPrompt = fmt.Sprintf(`Generate a concise git commit message for the following staged changes.

IMPORTANT: Do NOT use any tools. Rely SOLELY on the staged diff provided below.

Follow these exact rules:
1. First, generate a short title starting with an action word (Adds, Updates, Deletes, Renames)
2. Title must be under 72 characters, no colons, no markdown
3. Title should not include filenames
4. Then generate a concise description paragraph (be brief but informative)
5. Description should not include code blocks or filenames
6. No markdown formatting anywhere
7. Format: [Title]\n\n[Description]

Staged changes:
%s

Please generate only the commit message content, no additional commentary.`, string(diffOutput))
	}

	fmt.Println("🤖 Generating commit message with AI...")
	commitMessage, err := h.chatAgent.ProcessQuery(commitPrompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %v", err)
	}

	return strings.TrimSpace(commitMessage), nil
}

// HandleCommitConfirmation handles the commit message preview, editing, and confirmation
func (h *CommitMessageHandler) HandleCommitConfirmation(commitMessage string, filename string) (string, bool, error) {
	for {
		// Show preview
		fmt.Println("\n📋 Commit message preview:")
		fmt.Println("=============================================")
		fmt.Println(commitMessage)
		fmt.Println("=============================================")

		// Prompt for action
		if filename != "" {
			fmt.Printf("\n💡 Commit with this message for %s? (y)es/(n)o/(e)dit/(r)etry: ", filename)
		} else {
			fmt.Println("\n💡 Commit with this message? (y)es/(n)o/(e)dit/(r)etry:")
		}

		input, _ := h.reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "y", "yes":
			return commitMessage, true, nil
		case "n", "no":
			fmt.Println("❌ Commit cancelled")
			return "", false, nil
		case "e", "edit":
			editedMessage, err := h.EditCommitMessage(commitMessage)
			if err != nil {
				fmt.Printf("❌ Failed to edit commit message: %v\n", err)
				continue
			}
			commitMessage = editedMessage
		case "r", "retry":
			fmt.Println("🔄 Retrying commit message generation...")
			return "", true, nil // Signal to retry generation
		default:
			fmt.Println("❌ Invalid input. Please enter y, n, e, or r")
		}
	}
}

// EditCommitMessage opens the default editor to edit the commit message
func (h *CommitMessageHandler) EditCommitMessage(commitMessage string) (string, error) {
	// Write commit message to temporary file
	tempFile := "commit_msg_edit.txt"
	err := os.WriteFile(tempFile, []byte(commitMessage), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary commit message file: %v", err)
	}
	defer os.Remove(tempFile)

	// Determine default editor (use $EDITOR or fallback to vi)
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	// Open editor
	cmd := exec.Command(editor, tempFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("📝 Opening %s to edit commit message...\n", editor)
	fmt.Println("💡 Make your changes, save, and exit the editor to continue")

	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("failed to edit commit message: %v", err)
	}

	// Read edited message
	editedContent, err := os.ReadFile(tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to read edited commit message: %v", err)
	}

	editedMessage := strings.TrimSpace(string(editedContent))
	if editedMessage == "" {
		return "", fmt.Errorf("commit message cannot be empty")
	}

	fmt.Println("✅ Commit message edited successfully")
	return editedMessage, nil
}

// CreateCommit creates the git commit with the given message
func (h *CommitMessageHandler) CreateCommit(commitMessage string) error {
	// Write commit message to temporary file
	tempFile := "commit_msg.txt"
	err := os.WriteFile(tempFile, []byte(commitMessage), 0644)
	if err != nil {
		return fmt.Errorf("failed to create temporary commit message file: %v", err)
	}
	defer os.Remove(tempFile)

	fmt.Println("\n💾 Creating commit...")
	cmd := exec.Command("git", "commit", "-F", tempFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create commit: %v", err)
	}

	fmt.Printf("✅ Commit created successfully!\n")
	fmt.Printf("Output: %s\n", string(output))
	return nil
}
