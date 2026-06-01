package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

var nonWhitespaceTokenRegex = regexp.MustCompile(`\S+`)

// Default shell output truncation limits (raised from 700 to 2500 total tokens)
// Debug builds and complex commands often produce lots of output
const defaultShellHeadTokenLimit = 800  // head: 800 tokens
const defaultShellTailTokenLimit = 1700 // tail: 1700 tokens

// getShellOutputTokenLimits returns head and tail token limits from config or defaults
func getShellOutputTokenLimits() (head, tail int) {
	head = defaultShellHeadTokenLimit
	tail = defaultShellTailTokenLimit

	if raw := configuration.GetEnvSimple("SHELL_HEAD_TOKENS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			head = parsed
		}
	}
	if raw := configuration.GetEnvSimple("SHELL_TAIL_TOKENS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			tail = parsed
		}
	}
	return head, tail
}

// executeShellCommandWithTruncation handles shell command execution with smart truncation and deduplication
func (a *Agent) executeShellCommandWithTruncation(ctx context.Context, command string) (string, error) {
	// Wire TerminalManager into context for WebUI mode
	// (nil-safe: WithTerminalManager handles nil TerminalAccess)
	if tm := a.terminalManager; tm != nil {
		ctx = tools.WithTerminalManager(ctx, tm)
	}

	// Wire BackgroundProcessManager into context for CLI mode (lazy-init)
	if a.terminalManager == nil {
		if a.backgroundProcessManager == nil {
			a.backgroundProcessManager = tools.NewBackgroundProcessManager()
		}
		ctx = tools.WithBackgroundProcessManager(ctx, a.backgroundProcessManager)
	}

	headTokenLimit, tailTokenLimit := getShellOutputTokenLimits()

	// Check if we've run this exact command before
	if prevResult, exists := a.GetShellCommandHistoryEntry(command); exists {
		// Command was run before - mark the previous occurrence as stale in conversation
		a.updatePreviousShellCommandMessage(prevResult)
	}

	a.Logger().Debug("Executing shell command: %s\n", command)

	fullResult, err := tools.ExecuteShellCommand(ctx, command)

	// Diff the workspace against the ChangeTracker's cached baseline and
	// record any file mutations the shell introduced (sed -i, mv, rm,
	// cp, tee, etc.). The tracker keeps a long-lived snapshot rebased
	// after every call, so the typical cost per shell_command is one
	// stat-only walk (~5–20 ms on a 5000-file workspace) rather than
	// two full content reads (~280 ms each). Cold prime happens once
	// at EnableChangeTracking time.
	//
	// Short-circuit: skip the walk entirely for shell commands that
	// shellLooksReadOnly can prove make no filesystem changes (ls,
	// grep, cat, git status, …). Conservative — anything ambiguous
	// (redirects, chaining, unknown programs) falls through to the
	// full walk. Saves ~10 ms × however many read-only shells the
	// agent runs (typically the majority).
	//
	// Runs unconditionally on the post-side (even when the shell
	// errored) — a partial command may still have written something
	// we want to remember.
	if tracker := a.GetChangeTracker(); tracker != nil && tracker.IsEnabled() {
		if !shellLooksReadOnly(command) {
			tracker.TrackShellTurn(a.effectiveCwd(), "shell_command")
		}
	}

	a.Logger().Debug("Shell command result: %s, error: %v\n", fullResult, err)

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
			a.Logger().Debug("Warning: failed to save full shell output: %v\n", err)
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

	// Redact secrets from the output before returning to LLM
	if a.security != nil {
		redactor := a.security.GetOutputRedactor()
		if redactor != nil {
			result := redactor.RedactToolOutput(returnResult, "shell_command", map[string]interface{}{
				"command": command,
			})
			if len(result.Secrets) > 0 {
				a.Logger().Info("Shell output redacted: %d -> %d chars", len(returnResult), len(result.Content))
			}
			returnResult = result.Content
		}
	}

	// Update the tracked shell working directory for cd commands.
	a.updateShellCwd(command)

	// Store in history for potential deduplication
	a.SetShellCommandHistoryEntry(command, &ShellCommandResult{
		Command:         command,
		FullOutput:      fullResult,
		TruncatedOutput: returnResult,
		Error:           err,
		ExecutedAt:      time.Now().Unix(),
		MessageIndex:    len(a.state.GetMessages()), // Will be the next message index
		WasTruncated:    wasTruncated,
		FullOutputPath:  fullOutputPath,
		TruncatedTokens: truncatedTokens,
		TruncatedLines:  truncatedLines,
	})

	// Also record as a task action for conversation summary
	a.AddTaskAction("command_executed", fmt.Sprintf("Executed: %s", command), command)

	if err != nil {
		return returnResult, fmt.Errorf("failed to execute shell command: %w", err)
	}
	return returnResult, nil
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
	dir := ".sprout/shell_outputs"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create shell output directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("shell_output_%s_%d.txt", timestamp, time.Now().UnixNano()%1_000_000)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return "", fmt.Errorf("failed to write shell output file: %w", err)
	}

	return path, nil
}

// updatePreviousShellCommandMessage updates a previous shell command message to be brief
func (a *Agent) updatePreviousShellCommandMessage(prevResult *ShellCommandResult) {
	// Find the message in the conversation history
	messages := a.state.GetMessages()
	if prevResult.MessageIndex >= 0 && prevResult.MessageIndex < len(messages) {
		msg := &messages[prevResult.MessageIndex]

		// Update the message content to indicate it's stale
		staleMessage := fmt.Sprintf("Tool call result for shell_command: %s\n[STALE] This output is from an earlier execution - command was run again with potentially different results", prevResult.Command)

		// Update the message content
		if msg.Role == "user" && strings.Contains(msg.Content, "Tool call result for shell_command") {
			msg.Content = staleMessage
		}
	}
}

// checkBackgroundOutput retrieves accumulated output for a background shell session.
func (a *Agent) checkBackgroundOutput(ctx context.Context, sessionID string) (string, error) {
	// Wire TerminalManager into context for WebUI mode
	if tm := a.terminalManager; tm != nil {
		ctx = tools.WithTerminalManager(ctx, tm)
	}

	// Wire BackgroundProcessManager into context for CLI mode (lazy-init)
	if a.terminalManager == nil {
		if a.backgroundProcessManager == nil {
			a.backgroundProcessManager = tools.NewBackgroundProcessManager()
		}
		ctx = tools.WithBackgroundProcessManager(ctx, a.backgroundProcessManager)
	}

	result, err := tools.CheckBackgroundOutput(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to check background session %s: %w", sessionID, err)
	}
	return result, nil
}

// stopBackgroundSession terminates a background shell session by session ID.
func (a *Agent) stopBackgroundSession(sessionID string) (string, error) {
	// Try TerminalManager first (WebUI mode)
	if tm := a.terminalManager; tm != nil {
		if err := tm.StopBackgroundSession(sessionID); err != nil {
			return "", fmt.Errorf("failed to stop background session %s: %w", sessionID, err)
		}
		return fmt.Sprintf("Background session %s stopped successfully", sessionID), nil
	}

	// Fallback to BackgroundProcessManager (CLI mode) — lazy-init if needed
	if a.backgroundProcessManager == nil {
		a.backgroundProcessManager = tools.NewBackgroundProcessManager()
	}
	if err := a.backgroundProcessManager.Stop(sessionID); err != nil {
		return "", fmt.Errorf("failed to stop background session %s: %w", sessionID, err)
	}
	return fmt.Sprintf("Background session %s stopped successfully", sessionID), nil
}

// executeShellCommandBackground executes a shell command in a background session
// and returns immediately with the session ID. This is for long-running commands
// that should not block the agent.
func (a *Agent) executeShellCommandBackground(ctx context.Context, command string) (string, error) {
	// Wire TerminalManager into context for WebUI mode
	if tm := a.terminalManager; tm != nil {
		ctx = tools.WithTerminalManager(ctx, tm)
	}

	// Wire BackgroundProcessManager into context for CLI mode (lazy-init)
	if a.terminalManager == nil {
		if a.backgroundProcessManager == nil {
			a.backgroundProcessManager = tools.NewBackgroundProcessManager()
		}
		ctx = tools.WithBackgroundProcessManager(ctx, a.backgroundProcessManager)
	}

	a.Logger().Debug("Executing shell command in background: %s\n", command)

	result, err := tools.ExecuteShellCommandBackground(ctx, command, a.GetSessionID())
	a.Logger().Debug("Background shell command result: %s, error: %v\n", result, err)

	// Record as a task action for conversation summary
	a.AddTaskAction("command_background_executed", fmt.Sprintf("Started background command: %s", command), command)

	if err != nil {
		return "", fmt.Errorf("failed to execute background shell command: %w", err)
	}
	return result, nil
}
