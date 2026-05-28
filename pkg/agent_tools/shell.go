package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ExecuteShellCommand executes a shell command with safety checks
func ExecuteShellCommand(ctx context.Context, command string) (string, error) {
	return ExecuteShellCommandWithSafety(ctx, command, true, "", false)
}

// ExecuteShellCommandWithSafety executes a shell command with configurable safety checks.
// The streamOutput parameter controls whether output streams to terminal in real-time (true)
// or is captured silently (false, for LLM tool calls).
//
// Native builds use os/exec; the js/wasm build routes through pkg/wasmshell.
// The platform-specific implementation lives in shell_native.go / shell_js.go.
func ExecuteShellCommandWithSafety(ctx context.Context, command string, interactiveMode bool, sessionID string, streamOutput bool) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty command provided")
	}

	// Check for TerminalManager in context (WebUI mode). Skipped under WASM
	// because no terminal manager is ever installed there.
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
				// Check if the command was promoted to background due to timeout
				if strings.HasPrefix(err.Error(), "COMMAND_PROMOTED_TO_BACKGROUND:") {
					bgSessionID := strings.TrimPrefix(err.Error(), "COMMAND_PROMOTED_TO_BACKGROUND:")
					msg := formatBackgroundPromotionMessage(bgSessionID, command, output)
					return msg, nil
				}
				// Fall through to os/exec on other PTY errors
				log.Printf("debug: PTY command execution failed on session %q, falling back to os/exec: %v", hiddenSessionID, err)
			} else {
				// Build final output with status
				finalOutput := buildShellOutputWithStatus(output, command, exitCode, nil)

				// Print truncated preview unless in tests/CI
				truncatedOutput := truncateOutput(output, 8)
				if truncatedOutput != "" && shouldPrintCapturedShellPreview() {
					fmt.Printf("%s\n", truncatedOutput)
				}

				return finalOutput, nil
			}
		}
	}

	// NOTE: Security validation is handled by the static classifier in security_classifier.go, invoked at the tool registry level
	return runShellCommand(ctx, command, streamOutput)
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
		return "", fmt.Errorf("execute background command: %w", err)
	}

	// Return JSON result with session ID
	resultBytes, err := json.Marshal(map[string]string{
		"session_id": bgSessionID,
		"status":     "running",
	})
	if err != nil {
		return "", fmt.Errorf("marshal background result: %w", err)
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
		return "", fmt.Errorf("marshal check result: %w", err)
	}
	return string(resultBytes), nil
}

// formatBackgroundPromotionMessage creates a formatted message for commands that
// were promoted to background sessions due to timeout.
func formatBackgroundPromotionMessage(sessionID, command, accumulatedOutput string) string {
	// Truncate accumulated output preview. Note: this is a SUBSET of the
	// command's full output. Output past this point lives only in the
	// background session and must be fetched via check_background.
	preview := accumulatedOutput
	const maxPreview = 2000
	previewTruncated := false
	if len(preview) > maxPreview {
		preview = preview[:maxPreview] + "\n... (preview truncated)"
		previewTruncated = true
	}

	caveat := "IMPORTANT: the output above is partial — only what arrived before the 2-minute tool deadline. The command kept running."
	if previewTruncated {
		caveat = "IMPORTANT: the output above is doubly partial — only what arrived before the 2-minute tool deadline, AND only the first ~2KB of that. The command kept running."
	}

	return fmt.Sprintf(
		"Command exceeded the 2-minute tool deadline. It is still running in background session %s.\n\n"+
			"Command: %s\n\n"+
			"Output so far (partial):\n%s\n\n"+
			"%s\n\n"+
			"To get the rest, do NOT assume the command finished — actively poll:\n"+
			"- Check progress (returns accumulated output since session start): shell_command check_background=\"%s\"\n"+
			"- Stop it (kills the process): shell_command stop_background=\"%s\"\n\n"+
			"Background sessions are kept for up to 2 hours of inactivity. Either wait and poll, "+
			"or stop the command if you want to try a different approach.",
		sessionID, command, preview, caveat, sessionID, sessionID,
	)
}
