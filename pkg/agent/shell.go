package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

var nonWhitespaceTokenRegex = regexp.MustCompile(`\S+`)

// executeShellCommandWithTruncation handles shell command execution with smart truncation and deduplication
func (a *Agent) executeShellCommandWithTruncation(ctx context.Context, command string) (string, error) {
	const (
		headTokenLimit = 200
		tailTokenLimit = 500
	)

	// Check if we've run this exact command before
	if prevResult, exists := a.shellCommandHistory[command]; exists {
		// Command was run before - mark the previous occurrence as stale in conversation
		a.updatePreviousShellCommandMessage(prevResult)
	}

	a.ToolLog("executing command", command)
	a.debugLog("Executing shell command: %s\n", command)

	fullResult, err := tools.ExecuteShellCommand(ctx, command)
	a.debugLog("Shell command result: %s, error: %v\n", fullResult, err)

	// Determine what to return (truncated or full)
	returnResult := fullResult
	wasTruncated := false
	fullOutputPath := ""
	truncatedTokens := 0
	truncatedLines := 0

	tokenIndices := nonWhitespaceTokenRegex.FindAllStringIndex(fullResult, -1)
	totalTokens := len(tokenIndices)

	if totalTokens > headTokenLimit+tailTokenLimit {
		topTokens := headTokenLimit
		bottomTokens := tailTokenLimit

		topEndIndex := tokenIndices[topTokens-1][1]
		bottomStartIndex := tokenIndices[totalTokens-bottomTokens][0]

		topSegment := fullResult[:topEndIndex]
		bottomSegment := fullResult[bottomStartIndex:]
		middleSegment := fullResult[topEndIndex:bottomStartIndex]

		truncatedTokens = totalTokens - (topTokens + bottomTokens)
		truncatedLines = countLinesInSegment(middleSegment)
		if truncatedLines == 0 && truncatedTokens > 0 {
			truncatedLines = 1
		}

		var saveErr error
		if outputPath, err := a.saveShellOutputToFile(fullResult); err != nil {
			saveErr = err
			a.debugLog("Warning: failed to save full shell output: %v\n", err)
		} else {
			fullOutputPath = outputPath
		}

		truncationNotice := buildTruncationNotice(topTokens, bottomTokens, truncatedTokens, truncatedLines, fullOutputPath, saveErr)

		var builder strings.Builder
		builder.WriteString(topSegment)
		if !strings.HasSuffix(topSegment, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
		builder.WriteString(truncationNotice)
		builder.WriteString("\n\n")
		builder.WriteString(bottomSegment)

		returnResult = builder.String()
		wasTruncated = true
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
		FullOutputPath:  fullOutputPath,
		TruncatedTokens: truncatedTokens,
		TruncatedLines:  truncatedLines,
	}

	return returnResult, err
}

func countLinesInSegment(segment string) int {
	if len(segment) == 0 {
		return 0
	}

	lines := strings.Count(segment, "\n")
	if !strings.HasSuffix(segment, "\n") {
		lines++
	}

	return lines
}

func buildTruncationNotice(headTokens, tailTokens, truncatedTokens, truncatedLines int, outputPath string, saveErr error) string {
	if outputPath == "" {
		if saveErr != nil {
			return fmt.Sprintf("[Output truncated: omitted %d middle token(s) across ~%d line(s). Showing first %d tokens and last %d tokens. Failed to save full output: %v]",
				truncatedTokens, truncatedLines, headTokens, tailTokens, saveErr)
		}
		return fmt.Sprintf("[Output truncated: omitted %d middle token(s) across ~%d line(s). Showing first %d tokens and last %d tokens. Full output path unavailable]",
			truncatedTokens, truncatedLines, headTokens, tailTokens)
	}

	return fmt.Sprintf("[Output truncated: omitted %d middle token(s) across ~%d line(s). Showing first %d tokens and last %d tokens. Full output saved to %s]",
		truncatedTokens, truncatedLines, headTokens, tailTokens, outputPath)
}

func (a *Agent) saveShellOutputToFile(output string) (string, error) {
	dir := ".ledit"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("shell_output_%s_%d.txt", timestamp, time.Now().UnixNano()%1_000_000)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return "", err
	}

	return path, nil
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
