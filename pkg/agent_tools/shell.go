package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// ExecuteShellCommand executes a shell command with safety checks
func ExecuteShellCommand(ctx context.Context, command string) (string, error) {
	return ExecuteShellCommandWithSafety(ctx, command, true, "", false)
}

// ExecuteShellCommandWithSafety executes a shell command with configurable safety checks.
// The streamOutput parameter controls whether output streams to terminal in real-time (true)
// or is captured silently (false, for LLM tool calls).
func ExecuteShellCommandWithSafety(ctx context.Context, command string, interactiveMode bool, sessionID string, streamOutput bool) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty command provided")
	}

	// Check for TerminalManager in context (WebUI mode)
	if tm := TerminalManagerFromContext(ctx); tm != nil && !streamOutput {
		// Route through hidden PTY session
		// Use sessionID as the chat identifier; generate one if not set
		chatID := sessionID
		if chatID == "" {
			chatID = "default"
		}

		// Get or create a hidden session for this chat
		hiddenSessionID, err := tm.GetOrCreateHiddenSessionForChat(ctx, chatID)
		if err != nil {
			// Fall through to os/exec on failure — don't break agent execution
			log.Printf("debug: PTY session creation failed for chat %q, falling back to os/exec: %v", chatID, err)
		} else {
			// Execute via hidden PTY
			output, exitCode, err := tm.ExecuteCommandInHidden(ctx, hiddenSessionID, command)
			if err != nil {
				// Fall through to os/exec on PTY error
				log.Printf("debug: PTY command execution failed on session %q, falling back to os/exec: %v", hiddenSessionID, err)
			} else {
				// Build final output with status
				finalOutput := buildShellOutputWithStatus(output, command, exitCode, nil)

				// Print truncated preview unless in tests/CI
				truncatedOutput := truncateOutput(output, 2)
				if truncatedOutput != "" && shouldPrintCapturedShellPreview() {
					fmt.Printf("%s\n", truncatedOutput)
				}

				return finalOutput, nil
			}
		}
	}

	// NOTE: Security validation is handled by the static classifier in security.go, invoked at the tool registry level

	// Create command with context
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", command)

	// Explicitly set working directory to the workspace carried on the context.
	if wd := filesystem.WorkspaceRootFromContext(ctx); wd != "" {
		cmd.Dir = wd
	} else if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}

	if streamOutput {
		// STREAMING MODE: Use pipes for real-time output
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

	// SILENT MODE: Capture output without streaming (for LLM tool calls)
	// CombinedOutput returns stdout+stderr together
	outputBytes, err := cmd.CombinedOutput()

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

	output := string(outputBytes)

	// For LLM tool calls, truncate output to 2 lines
	truncatedOutput := truncateOutput(output, 2)

	// Print truncated output to terminal unless we're in tests/CI.
	if truncatedOutput != "" && shouldPrintCapturedShellPreview() {
		fmt.Printf("%s\n", truncatedOutput)
	}

	// Build the final output with status header
	finalOutput := buildShellOutputWithStatus(output, command, exitCode, err)

	return finalOutput, nil
}

func shouldPrintCapturedShellPreview() bool {
	if os.Getenv("CI") != "" {
		return false
	}

	executable := filepath.Base(os.Args[0])
	if strings.HasSuffix(executable, ".test") {
		return false
	}

	return true
}

// truncateOutput limits output to a specified number of lines
func truncateOutput(output string, maxLines int) string {
	if output == "" {
		return ""
	}

	// Edge case: if maxLines is 0 or negative, return empty string
	if maxLines <= 0 {
		return ""
	}

	lines := strings.Split(output, "\n")

	if len(lines) <= maxLines {
		// Output is short enough, return as-is
		return strings.TrimSpace(output)
	}

	// Truncate to maxLines and add truncation indicator
	visibleLines := lines[:maxLines]
	truncated := strings.Join(visibleLines, "\n")
	return fmt.Sprintf("%s\n... (truncated, %d more lines)", strings.TrimSpace(truncated), len(lines)-maxLines)
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
		icon = "[OK]"
	} else {
		status = "FAILED"
		icon = "[FAIL]"
	}

	// Build status header
	header := fmt.Sprintf("%s Command completed with exit code %d (%s)\n", icon, exitCode, status)

	return header + "(no output)"
}

// ExecuteShellCommandBackground runs a command in a background hidden PTY session
// and returns a JSON result with the session ID. Only works in WebUI mode (requires TerminalManager).
// This is for commands that should run asynchronously without waiting for completion.
func ExecuteShellCommandBackground(ctx context.Context, command string, sessionID string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty command provided")
	}

	// Background mode requires TerminalManager (WebUI mode only)
	tm := TerminalManagerFromContext(ctx)
	if tm == nil {
		return "", fmt.Errorf("background mode requires WebUI terminal manager")
	}

	// Use sessionID as the chat identifier; generate one if not set
	chatID := sessionID
	if chatID == "" {
		chatID = "default"
	}

	// Execute in background
	bgSessionID, err := tm.ExecuteCommandInBackground(ctx, chatID, command)
	if err != nil {
		return "", fmt.Errorf("failed to execute background command: %w", err)
	}

	// Return JSON result with session ID
	resultBytes, err := json.Marshal(map[string]string{
		"session_id": bgSessionID,
		"status":     "running",
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal background result: %w", err)
	}
	return string(resultBytes), nil
}

// CheckBackgroundOutput retrieves accumulated output for a background session.
// Returns JSON with session_id, status, and output fields.
func CheckBackgroundOutput(ctx context.Context, sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("empty session_id provided")
	}

	// Requires TerminalManager (WebUI mode only)
	tm := TerminalManagerFromContext(ctx)
	if tm == nil {
		return "", fmt.Errorf("background output retrieval requires WebUI terminal manager")
	}

	// Get the output
	output, err := tm.GetBackgroundOutput(sessionID)
	if err != nil {
		return "", err
	}

	// Check if the session is still active via the interface
	status := "running"
	if !tm.IsSessionActive(sessionID) {
		status = "exited"
	}

	resultBytes, err := json.Marshal(map[string]string{
		"session_id": sessionID,
		"status":     status,
		"output":     output,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal check result: %w", err)
	}
	return string(resultBytes), nil
}
