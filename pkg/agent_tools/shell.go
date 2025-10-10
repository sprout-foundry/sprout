package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// ExecuteShellCommand executes a shell command with safety checks
func ExecuteShellCommand(ctx context.Context, command string) (string, error) {
	return ExecuteShellCommandWithSafety(ctx, command, true, "")
}

// ExecuteShellCommandWithSafety executes a shell command with configurable safety checks
func ExecuteShellCommandWithSafety(ctx context.Context, command string, interactiveMode bool, sessionID string) (string, error) {
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

	// Create command with context
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", command)

	// Get pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command: %w", err)
	}

	// Create a string builder to capture the output
	var output strings.Builder

	// Create scanners to read the output line by line
	stdoutScanner := bufio.NewScanner(stdout)
	stderrScanner := bufio.NewScanner(stderr)

	// Read from stdout and stderr concurrently
	go func() {
		for stdoutScanner.Scan() {
			line := stdoutScanner.Text()
			output.WriteString(line + "\n")
		}
	}()
	go func() {
		for stderrScanner.Scan() {
			line := stderrScanner.Text()
			output.WriteString(line + "\n")
		}
	}()

	// Wait for the command to finish
	err = cmd.Wait()
	
	// Get the exit code for status reporting
	exitCode := 0
	if err != nil {
		// Check if it's an exit error (command ran but failed)
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			}
		}
	}
	
	// Build the final output with status header
	finalOutput := buildShellOutputWithStatus(output.String(), command, exitCode, err)
	
	if err != nil {
		// Return the enhanced output with error information
		if exitCode != 0 {
			return finalOutput, fmt.Errorf("command failed with exit code %d", exitCode)
		}
		return finalOutput, fmt.Errorf("command failed: %w", err)
	}

	return finalOutput, nil
}

// trackFileDeletion records file deletion commands in the changelog
func trackFileDeletion(command string, sessionID string) {
	// TODO: Implement file deletion tracking in changelog
	// This will need to integrate with the existing changelog system
	fmt.Printf("📝 Tracking file deletion: %s (session: %s)\n", command, sessionID)
}

// buildShellOutputWithStatus enhances shell output with status information
func buildShellOutputWithStatus(output, command string, exitCode int, err error) string {
	// If there's substantial output or an error, just return the output as-is
	// This preserves the original behavior for most cases
	if strings.TrimSpace(output) != "" || err != nil {
		return output
	}
	
	// For successful commands with no output, add a status header
	var status string
	var icon string
	if exitCode == 0 {
		status = "SUCCESS"
		icon = "✅"
	} else {
		status = "FAILED"
		icon = "❌"
	}
	
	// Build status header
	header := fmt.Sprintf("%s Command completed with exit code %d (%s)\n", icon, exitCode, status)
	
	// If there was any output (even whitespace), include it after the header
	if strings.TrimSpace(output) == "" {
		return header + "(no output)"
	}
	
	return header + output
}
