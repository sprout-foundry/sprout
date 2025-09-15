package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent_tools"
)

// executeShellCommandWithTruncation handles shell command execution with smart truncation and deduplication
func (a *Agent) executeShellCommandWithTruncation(command string) (string, error) {
	const maxOutputLength = 20000 // 20K character limit

	// Check if we've run this exact command before
	if prevResult, exists := a.shellCommandHistory[command]; exists {
		// Command was run before - mark the previous occurrence as stale in conversation
		a.updatePreviousShellCommandMessage(prevResult)
	}

	// ALWAYS execute the command fresh to get current state</

	// Check circuit breaker for test commands that might be failing repeatedly
	if strings.Contains(command, "go test") || strings.Contains(command, "test") {
		if blocked, warning := a.CheckCircuitBreaker("test_command", command, 4); blocked {
			return warning, fmt.Errorf("circuit breaker triggered - too many failed test attempts")
		}
	}

	a.ToolLog("executing command", command)
	a.debugLog("Executing shell command: %s\n", command)

	fullResult, err := tools.ExecuteShellCommand(command)
	a.debugLog("Shell command result: %s, error: %v\n", fullResult, err)

	// Determine what to return (truncated or full)
	var returnResult string
	var wasTruncated bool

	if len(fullResult) > maxOutputLength {
		returnResult = fullResult[:maxOutputLength] + fmt.Sprintf("\n\n... (output truncated at %d chars, full output was %d chars)", maxOutputLength, len(fullResult))
		wasTruncated = true
	} else {
		returnResult = fullResult
		wasTruncated = false
	}

	// Store in history for potential deduplication
	a.shellCommandHistory[command] = &ShellCommandResult{
		Command:         command,
		FullOutput:      fullResult,
		TruncatedOutput: returnResult,
		Error:           err,
		ExecutedAt:      time.Now().Unix(),
		MessageIndex:    len(a.messages), // Will be the next message index
		WasTruncated:    wasTruncated,
	}

	return returnResult, err
}

// updatePreviousShellCommandMessage updates a previous shell command message to be brief
func (a *Agent) updatePreviousShellCommandMessage(prevResult *ShellCommandResult) {
	// Find the message in the conversation history
	if prevResult.MessageIndex >= 0 && prevResult.MessageIndex < len(a.messages) {
		msg := &a.messages[prevResult.MessageIndex]

		// Update the message content to indicate it's stale
		staleMessage := fmt.Sprintf("Tool call result for shell_command: %s\n[STALE] This output is from an earlier execution - command was run again with potentially different results", prevResult.Command)

		// Update the message content
		if msg.Role == "user" && strings.Contains(msg.Content, "Tool call result for shell_command") {
			msg.Content = staleMessage
		}
	}
}
