package tools

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ExecuteShellCommand executes a shell command with safety checks
func ExecuteShellCommand(command string) (string, error) {
	return ExecuteShellCommandWithSafety(command, true, "")
}

// ExecuteShellCommandWithSafety executes a shell command with configurable safety checks
func ExecuteShellCommandWithSafety(command string, interactiveMode bool, sessionID string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty command provided")
	}

	// Check for destructive commands
	if destructiveCmd, isDestructive := IsDestructiveCommand(command); isDestructive {
		// In interactive mode, ask for confirmation
		if interactiveMode {
			fmt.Printf("⚠️  Potentially destructive command detected:\n")
			fmt.Printf("   Command: %s\n", command)
			fmt.Printf("   Risk Level: %s - %s\n", destructiveCmd.RiskLevel, destructiveCmd.Description)
			fmt.Printf("\n🤔 Do you want to proceed? (yes/no): ")

			reader := bufio.NewReader(os.Stdin)
			userResponse, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read user response: %v", err)
			}

			userResponse = strings.ToLower(strings.TrimSpace(userResponse))
			if userResponse != "yes" && userResponse != "y" {
				return "", fmt.Errorf("command execution cancelled by user")
			}
		}
		// In non-interactive mode, proceed without confirmation but track the action
	}

	// Track file deletions in changelog
	if IsFileDeletionCommand(command) && sessionID != "" {
		trackFileDeletion(command, sessionID)
	}

	// Create command with timeout
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", command)

	// Set up timeout
	timeout := 60 * time.Second // Increased from 30s to 60s for longer operations

	done := make(chan error, 1)
	var output []byte
	var err error

	go func() {
		output, err = cmd.CombinedOutput()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			// Check if it's an exit error (command ran but failed)
			if exitError, ok := err.(*exec.ExitError); ok {
				if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
					return string(output), fmt.Errorf("command failed with exit code %d: %s", status.ExitStatus(), string(output))
				}
			}
			return string(output), fmt.Errorf("command failed: %w", err)
		}
		return string(output), nil
	case <-time.After(timeout):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return "", fmt.Errorf("command timed out after %v", timeout)
	}
}

// trackFileDeletion records file deletion commands in the changelog
func trackFileDeletion(command string, sessionID string) {
	// TODO: Implement file deletion tracking in changelog
	// This will need to integrate with the existing changelog system
	fmt.Printf("📝 Tracking file deletion: %s (session: %s)\n", command, sessionID)
}
