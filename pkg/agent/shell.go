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
		// Command was run before - update the previous message to be brief and show full result in current response
		a.updatePreviousShellCommandMessage(prevResult)
		
		// Return the full output (will be truncated if needed)
		if prevResult.Error != nil {
			return prevResult.FullOutput, prevResult.Error
		}
		
		// If output is still too long, truncate it but mention it's a repeated command
		output := prevResult.FullOutput
		if len(output) > maxOutputLength {
			truncated := output[:maxOutputLength]
			return truncated + fmt.Sprintf("\n\n... (output truncated at %d chars - repeated command, full output was %d chars)", maxOutputLength, len(output)), nil
		}
		return output, nil
	}
	
	// Execute the command for the first time
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
		
		// Update the message content to be brief
		briefMessage := fmt.Sprintf("Tool result for shell_command (repeated): %s\n\n[This command was run again - see latest execution below for full output]", prevResult.Command)
		
		// Update the message content
		if msg.Role == "user" && strings.Contains(msg.Content, "Tool call result for shell_command") {
			msg.Content = briefMessage
		}
	}
}