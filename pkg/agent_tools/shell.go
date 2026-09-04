package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// resolveShellChatID returns the chat-scoped identifier shell sessions should
// use. Preference order: the ToolEnv ChatID (per-conversation scoping, wired
// from the agent's event metadata in WebUI mode), then an explicit sessionID
// parameter, then the legacy "default" bucket (CLI / tests).
//
// Per-chat scoping matters in the daemon: without it every chat shared one
// hidden PTY (cwd pollution across conversations) and the per-chat
// background-session cap was really per-client.
func resolveShellChatID(envChatID, sessionID string) string {
	if envChatID != "" {
		return envChatID
	}
	if sessionID != "" {
		return sessionID
	}
	return "default"
}

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
		// Route through hidden PTY session. The chat identifier prefers the
		// conversation-scoped ID from the context (WebUI per-chat scoping,
		// see WithShellChatID), then the sessionID parameter, then the
		// legacy "default" bucket (CLI / tests).
		chatID := ShellChatIDFromContext(ctx)
		if chatID == "" {
			chatID = sessionID
		}
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

				return finalOutput, nil
			}
		}
	}

	// NOTE: Security validation is handled by the static classifier in security_classifier.go, invoked at the tool registry level
	return runShellCommand(ctx, command, streamOutput)
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
// and returns a JSON result with the session ID. Works in WebUI mode (TerminalManager)
// and CLI mode (BackgroundProcessManager). This is for commands that should run
// asynchronously without waiting for completion.
func ExecuteShellCommandBackground(ctx context.Context, command string, sessionID string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty command provided")
	}

	// Try TerminalManager first (WebUI mode)
	if tm := TerminalManagerFromContext(ctx); tm != nil {
		// Per-conversation scoping (WebUI): the context chat ID wins, then
		// the sessionID parameter, then the legacy "default" bucket.
		chatID := ShellChatIDFromContext(ctx)
		if chatID == "" {
			chatID = sessionID
		}
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

	// Fallback to BackgroundProcessManager (CLI mode)
	if bpm := BackgroundProcessManagerFromContext(ctx); bpm != nil {
		// Resolve working directory from context
		dir := ""
		if wd := filesystem.WorkspaceRootFromContext(ctx); wd != "" {
			dir = wd
		} else if wd, err := os.Getwd(); err == nil {
			dir = wd
		}

		bspSessionID, err := bpm.Start(ctx, command, dir)
		if err != nil {
			return "", fmt.Errorf("execute background command: %w", err)
		}
		resultBytes, err := json.Marshal(map[string]string{
			"session_id": bspSessionID,
			"status":     "running",
		})
		if err != nil {
			return "", fmt.Errorf("marshal background result: %w", err)
		}
		return string(resultBytes), nil
	}

	return "", fmt.Errorf("background command requires a TerminalManager (WebUI) or BackgroundProcessManager (CLI) attached to the agent context")
}

// maxBackgroundWaitSeconds caps the wait_seconds parameter on
// CheckBackgroundOutputWait. Picked to match Claude Code's default Bash
// timeout — long enough to collapse most polling on multi-hour workflows,
// short enough that the user can interrupt or redirect the agent without
// being parked indefinitely.
const maxBackgroundWaitSeconds = 600

// maxWakeupTimeoutSeconds caps the wakeup_timeout parameter on background
// shell sessions. Same value as maxBackgroundWaitSeconds: long enough to be
// a useful "check back later" deadline for multi-hour workflows, short enough
// that a typo'd value can't park the agent or overflow time.Duration.
const maxWakeupTimeoutSeconds = 600

// backgroundWaitTick is the internal polling interval used while waiting for
// a background session to exit. It only touches in-process state (session
// flag, file size) so it doesn't burn LLM tokens — the cost lives entirely
// inside this process.
const backgroundWaitTick = 500 * time.Millisecond

// CheckBackgroundOutput retrieves accumulated output for a background session.
// Returns JSON with session_id, status, and output fields.
// Works in WebUI mode (TerminalManager) and CLI mode (BackgroundProcessManager).
//
// Equivalent to CheckBackgroundOutputWait(ctx, sessionID, 0).
func CheckBackgroundOutput(ctx context.Context, sessionID string) (string, error) {
	return CheckBackgroundOutputWait(ctx, sessionID, 0)
}

// CheckBackgroundOutputWait is like CheckBackgroundOutput but blocks (up to
// waitSeconds, capped at maxBackgroundWaitSeconds) until the session exits or
// the wait elapses, then returns the snapshot. waitSeconds <= 0 means return
// immediately.
//
// A blocking wait is an LLM-side cost optimization: a 4-hour autonomous run
// polled every minute = ~240 round trips, each re-sending the full context.
// One blocking wait per 10 minutes collapses that to ~24, and an early exit
// returns as soon as the workflow finishes.
func CheckBackgroundOutputWait(ctx context.Context, sessionID string, waitSeconds int) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("empty session_id provided")
	}

	if waitSeconds > maxBackgroundWaitSeconds {
		waitSeconds = maxBackgroundWaitSeconds
	}

	// snapshot returns the latest output+status as a JSON string.
	// exited reports whether the background COMMAND has finished — for
	// TerminalManager sessions that's the sentinel (bgDone), not session
	// liveness, because the shell outlives the command.
	snapshot := func() (string, bool, error) {
		// Try TerminalManager first (WebUI mode)
		if tm := TerminalManagerFromContext(ctx); tm != nil {
			output, err := tm.GetBackgroundOutput(sessionID)
			if err != nil {
				return "", false, err
			}
			status := "running"
			exited := false
			if done, ok := tm.BackgroundDoneChan(sessionID); ok {
				select {
				case <-done:
					status = "exited"
					exited = true
				default:
				}
			} else {
				// Unknown session or a background session from before the
				// sentinel existed. Fall back to liveness (legacy behavior).
				if !tm.IsSessionActive(sessionID) {
					status = "exited"
					exited = true
				}
			}
			resultBytes, err := json.Marshal(map[string]string{
				"session_id": sessionID,
				"status":     status,
				"output":     output,
			})
			if err != nil {
				return "", false, fmt.Errorf("marshal check result: %w", err)
			}
			return string(resultBytes), exited, nil
		}

		// Fallback to BackgroundProcessManager (CLI mode)
		if bpm := BackgroundProcessManagerFromContext(ctx); bpm != nil {
			output, status, err := bpm.CheckOutput(sessionID)
			if err != nil {
				return "", false, err
			}
			resultBytes, err := json.Marshal(map[string]string{
				"session_id": sessionID,
				"status":     status,
				"output":     output,
			})
			if err != nil {
				return "", false, fmt.Errorf("marshal check result: %w", err)
			}
			return string(resultBytes), status == "exited", nil
		}

		return "", false, fmt.Errorf("background output retrieval requires a TerminalManager (WebUI) or BackgroundProcessManager (CLI) attached to the agent context")
	}

	if waitSeconds <= 0 {
		out, _, err := snapshot()
		return out, err
	}

	// TerminalManager sessions have an explicit completion channel (the
	// sentinel). Block on it directly instead of polling — the command's
	// exit is signalled, not inferred from liveness. An early exit returns
	// as soon as the command finishes instead of burning the full window.
	if tm := TerminalManagerFromContext(ctx); tm != nil {
		if done, ok := tm.BackgroundDoneChan(sessionID); ok {
			deadline := time.NewTimer(time.Duration(waitSeconds) * time.Second)
			defer deadline.Stop()
			select {
			case <-done:
			case <-deadline.C:
			case <-ctx.Done():
			}
			out, _, err := snapshot()
			return out, err
		}
		// Unknown session — fall through to the polling loop (snapshot()
		// will surface the not-found error on the first call).
	}

	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	ticker := time.NewTicker(backgroundWaitTick)
	defer ticker.Stop()

	for {
		out, exited, err := snapshot()
		if err != nil {
			return "", err
		}
		if exited {
			return out, nil
		}
		if time.Now().After(deadline) {
			return out, nil
		}
		select {
		case <-ctx.Done():
			// Return what we have rather than an error — the caller still
			// wants the most recent snapshot, and ctx cancellation is the
			// normal way to interrupt a wait.
			return out, nil
		case <-ticker.C:
			// keep polling
		}
	}
}

// promotedSessionPattern matches the session ID in a background promotion
// message ("... still running in background session bg-foo-1234.") produced
// by formatBackgroundPromotionMessage — both the WebUI PTY path and the CLI
// BPM adoption path emit this exact phrasing.
var promotedSessionPattern = regexp.MustCompile(`still running in background session ([A-Za-z0-9_-]+)`)

// ParsePromotedBackgroundSession extracts the background session ID from a
// tool result that was promoted past the 2-minute deadline. Returns ok=false
// for ordinary results. The shell handler uses this to attach a wakeup
// watcher to promoted sessions, matching the explicit background=true path.
//
// Session IDs are [A-Za-z0-9_-]; '.' is excluded so the sentence-ending
// period after the ID is not captured.
func ParsePromotedBackgroundSession(output string) (string, bool) {
	m := promotedSessionPattern.FindStringSubmatch(output)
	if len(m) != 2 {
		return "", false
	}
	return m[1], true
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
