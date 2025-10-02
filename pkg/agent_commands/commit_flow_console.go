package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// executeConsoleFlow runs the commit flow using simple console prompts
// This is used when running inside the agent console to avoid dropdown conflicts
func (cf *CommitFlow) executeConsoleFlow() error {
	// Check current git status first
	stagedFiles, unstagedFiles, err := cf.getGitStatus()
	if err != nil {
		return fmt.Errorf("failed to get git status: %w", err)
	}

	// Simplified path: if staged files exist, proceed directly; else prompt to select files
	if len(stagedFiles) > 0 {
		return cf.generateCommitMessageAndCommit()
	}

	if len(unstagedFiles) == 0 {
		fmt.Printf("\r\n📭 No changes to commit. Working directory is clean.\r\n")
		return nil
	}

	// Prompt to select files and then generate message
	return cf.selectFilesToCommitConsole()
}

// promptForFiles shows a list of files and lets user select multiple
func (cf *CommitFlow) promptForFiles(files []string, prompt string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}

	fmt.Printf("\r\n%s\r\n", prompt)
	fmt.Printf("================\r\n\r\n")

	// Display files with numbers
	for i, file := range files {
		fmt.Printf("%d. %s\r\n", i+1, file)
	}

	fmt.Printf("\r\nEnter file numbers separated by commas (e.g., 1,3,5)\r\n")
	fmt.Printf("Or 'all' to select all files, 'q' to cancel: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(strings.ToLower(input))

	if input == "q" {
		return nil, fmt.Errorf("selection cancelled")
	}

	if input == "all" {
		return files, nil
	}

	// Parse comma-separated numbers
	selected := []string{}
	parts := strings.Split(input, ",")

	for _, part := range parts {
		num, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			fmt.Printf("\r\nInvalid input: %s\r\n", part)
			continue
		}

		if num < 1 || num > len(files) {
			fmt.Printf("\r\nInvalid file number: %d\r\n", num)
			continue
		}

		selected = append(selected, files[num-1])
	}

	if len(selected) == 0 {
		fmt.Printf("\r\nNo files selected. Please try again.\r\n")
		return cf.promptForFiles(files, prompt)
	}

	return selected, nil
}

// promptYesNo asks a yes/no question
func (cf *CommitFlow) promptYesNo(question string, defaultYes bool) (bool, error) {
	defaultStr := "y/N"
	if defaultYes {
		defaultStr = "Y/n"
	}

	fmt.Printf("\r\n%s (%s): ", question, defaultStr)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return defaultYes, nil
	}

	return input == "y" || input == "yes", nil
}

// selectFilesToCommitConsole is the console version of selectFilesToCommit
func (cf *CommitFlow) selectFilesToCommitConsole() error {
	_, unstagedFiles, err := cf.getGitStatus()
	if err != nil {
		return err
	}

	if len(unstagedFiles) == 0 {
		fmt.Printf("\r\n📭 No unstaged files to select.\r\n")
		return nil
	}

	// Let user select files
	selectedFiles, err := cf.promptForFiles(unstagedFiles, "📝 Select Files to Commit")
	if err != nil {
		fmt.Printf("\r\nFile selection cancelled.\r\n")
		return nil
	}

	// Stage selected files
	fmt.Printf("\r\n📤 Staging %d file(s)...\r\n", len(selectedFiles))
	for _, file := range selectedFiles {
		cmd := exec.Command("git", "add", file)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to stage %s: %w\n%s", file, err, output)
		}
		fmt.Printf("   ✓ %s\r\n", file)
	}

	// Generate commit message
	return cf.generateCommitMessageAndCommit()
}

// singleFileCommitConsole is the console version of singleFileCommit
func (cf *CommitFlow) singleFileCommitConsole() error {
	// Get both staged and unstaged files
	stagedFiles, unstagedFiles, err := cf.getGitStatus()
	if err != nil {
		return err
	}

	// Combine all files
	allFiles := append(stagedFiles, unstagedFiles...)
	if len(allFiles) == 0 {
		fmt.Printf("\r\n📭 No files to commit.\r\n")
		return nil
	}

	// Show just one file selection
	fmt.Printf("\r\n📄 Select ONE file to commit:\r\n")
	fmt.Printf("============================\r\n\r\n")

	for i, file := range allFiles {
		fmt.Printf("%d. %s\r\n", i+1, file)
	}

	fmt.Printf("\r\nEnter file number (1-%d) or 'q' to quit: ", len(allFiles))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "q" || input == "Q" {
		fmt.Printf("\r\nFile selection cancelled.\r\n")
		return nil
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(allFiles) {
		fmt.Printf("\r\nInvalid selection. Please try again.\r\n")
		return cf.singleFileCommitConsole()
	}

	selectedFile := allFiles[choice-1]

	// Reset any staged files first
	fmt.Printf("\r\n🔄 Resetting staged files...\r\n")
	if err := exec.Command("git", "reset", "HEAD").Run(); err != nil {
		return fmt.Errorf("failed to reset staged files: %w", err)
	}

	// Stage only the selected file
	fmt.Printf("📤 Staging %s...\r\n", selectedFile)
	cmd := exec.Command("git", "add", selectedFile)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stage %s: %w\n%s", selectedFile, err, output)
	}

	// Generate commit message for single file
	return cf.generateCommitMessageAndCommit()
}

// stageAllAndCommitConsole is the console version (same as regular since no dropdown)
func (cf *CommitFlow) stageAllAndCommitConsole() error {
	fmt.Printf("\r\n🎯 Staging all modified files...\r\n")

	cmd := exec.Command("git", "add", "-A")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage files: %w", err)
	}

	fmt.Printf("✅ All files staged.\r\n")
	return cf.generateCommitMessageAndCommit()
}
