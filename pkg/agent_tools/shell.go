package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// ExecuteShellCommand executes a shell command with safety checks
func ExecuteShellCommand(ctx context.Context, command string) (string, error) {
	return ExecuteShellCommandWithSafety(ctx, command, true, "")
}

// ExecuteShellCommandWithSafety executes a shell command with configurable safety checks
// and streams output in real-time to the terminal.
func ExecuteShellCommandWithSafety(ctx context.Context, command string, interactiveMode bool, sessionID string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty command provided")
	}

	// NOTE: Security validation is now handled by the LLM-based validator at the tool registry level
	// This provides context-aware evaluation instead of regex pattern matching

	// Track file deletions in changelog (for change history, not security validation)
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

	// Buffer to capture output for return value
	var outputBuf bytes.Buffer

	// Stream stdout and stderr in real-time
	// Use goroutines to handle both concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	// Copy stdout to both terminal and buffer
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(os.Stdout, &outputBuf), stdout)
	}()

	// Copy stderr to both terminal and buffer
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(os.Stderr, &outputBuf), stderr)
	}()

	// Wait for both streams to finish
	wg.Wait()

	// Wait for command to complete
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
	finalOutput := buildShellOutputWithStatus(outputBuf.String(), command, exitCode, err)

	// Shell tool execution is always successful as long as we can run the command
	// Non-zero exit codes are normal command outcomes, not tool failures
	// The output includes the command's stderr and exit status information
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
	// If there's substantial output, just return the output as-is
	// This preserves the original behavior for most cases
	if strings.TrimSpace(output) != "" {
		return output
	}

	// For commands with no output, add a status header
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
