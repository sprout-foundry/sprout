package tools

// shell_handler_background.go — the background-process layer of the shell
// command handler: shell-mutation tracking (trackShellMutation,
// trackBackgroundMutation, bgSnapshotFinished), the check/stop/background
// sub-handlers, the sync sub-handler, the bgResult type, and the wakeup
// watcher (startWakeupWatcher). Split out of shell_handler.go.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// trackShellMutation records shell-caused filesystem mutations with the
// agent's ChangeTracker via the TrackShellCommand tool func. No-op when no
// tracker is wired (standalone handler use).
func trackShellMutation(env ToolEnv, command string) {
	if command == "" {
		return
	}
	if fn := env.ResolveToolFuncs().TrackShellCommand; fn != nil {
		if err := fn(command); err != nil {
			log.Printf("[shell_command] change tracking failed: %v", err)
		}
	}
}

// trackBackgroundMutation is trackShellMutation for a completed/stopped
// background session. Resolves the ORIGINAL command from the session
// registry when available so the ChangeTracker's destructive-command
// classifier (shellIsDestructive) sees `git reset --hard` instead of a
// synthetic "background session <id>" label — destructive classification
// switches the mutation walk into per-file, no-auto-skip mode, which is
// exactly the recovery coverage a background git revert needs.
func trackBackgroundMutation(env ToolEnv, sessionID, outcome string) {
	command := backgroundCommandFor(sessionID)
	if command == "" {
		command = "background session " + sessionID + " (" + outcome + ")"
	}
	trackShellMutation(env, command)
}

// bgSnapshotFinished reports whether a check_background snapshot JSON
// reports the background command has exited. Failures to parse count
// as not-finished so the mutation diff is skipped rather than run
// speculatively.
func bgSnapshotFinished(resultJSON string) bool {
	var snap struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &snap); err != nil {
		return false
	}
	return snap.Status == "exited"
}

// handleCheckBackground retrieves accumulated output for a background session.
// When waitSeconds > 0, it blocks (capped at maxBackgroundWaitSeconds) until
// the session exits or the wait elapses, then returns the snapshot.
//
// When the snapshot reports the background command has finished, the
// workspace mutation diff runs here: the command's writes happened
// between its issue and this observation, and this is the first point
// they can be captured and attributed correctly.
func (h *shellCommandHandler) handleCheckBackground(ctx context.Context, env ToolEnv, sessionID string, waitSeconds int) (ToolResult, error) {
	result, err := CheckBackgroundOutputWait(ctx, sessionID, waitSeconds)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("check background %q: %v", sessionID, err),
			IsError: true,
		}, agenterrors.NewTool("shell_command", fmt.Sprintf("check background %q: %v", sessionID, err), err)
	}

	if bgSnapshotFinished(result) {
		trackBackgroundMutation(env, sessionID, "completed")
	}

	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

// handleStopBackground terminates a background session.
func (h *shellCommandHandler) handleStopBackground(ctx context.Context, env ToolEnv, sessionID string) (ToolResult, error) {
	// Try TerminalManager first (WebUI mode)
	tm := TerminalManagerFromContext(ctx)
	if tm != nil {
		err := tm.StopBackgroundSession(sessionID)
		if err != nil {
			return ToolResult{
				Output:  fmt.Sprintf("stop background %q: %v", sessionID, err),
				IsError: true,
			}, agenterrors.NewTool("shell_command", fmt.Sprintf("stop background %q: %v", sessionID, err), err)
		}

		result := fmt.Sprintf("Background session %s stopped.", sessionID)
		if env.OutputWriter != nil {
			io.WriteString(env.OutputWriter, result)
		}

		// The killed command may have written files before termination;
		// record whatever landed. Best-effort, same as the sync path.
		trackBackgroundMutation(env, sessionID, "stopped")

		return ToolResult{
			Output:     result,
			TokenUsage: int64(estimateTokenUsage(result)),
		}, nil
	}

	// Fallback to BackgroundProcessManager (CLI mode)
	bpm := BackgroundProcessManagerFromContext(ctx)
	if bpm == nil {
		return ToolResult{
			Output:  "stop_background requires a TerminalManager (WebUI) or BackgroundProcessManager (CLI) attached to the agent context",
			IsError: true,
		}, agenterrors.NewTool("shell_command", "stop_background requires a TerminalManager (WebUI) or BackgroundProcessManager (CLI) attached to the agent context", nil)
	}

	err := bpm.Stop(sessionID, 10*time.Second)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("stop background %q: %v", sessionID, err),
			IsError: true,
		}, agenterrors.NewTool("shell_command", fmt.Sprintf("stop background %q: %v", sessionID, err), err)
	}

	result := fmt.Sprintf("Background session %s stopped.", sessionID)
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	// The killed command may have written files before termination;
	// record whatever landed. Best-effort, same as the sync path.
	trackBackgroundMutation(env, sessionID, "stopped")

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

// handleBackground runs a command in a background session.
func (h *shellCommandHandler) handleBackground(ctx context.Context, env ToolEnv, command string) (ToolResult, error) {
	result, err := ExecuteShellCommandBackground(ctx, command, "")
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("execute background command: %v", err),
			IsError: true,
		}, agenterrors.NewTool("shell_command", fmt.Sprintf("execute background command: %v", err), err)
	}

	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

// handleSync runs a command synchronously.
func (h *shellCommandHandler) handleSync(ctx context.Context, env ToolEnv, command string) (ToolResult, error) {
	// Execute with safety checks.
	// interactiveMode=false, streamOutput=false for agent tool calls.
	result, err := ExecuteShellCommandWithSafety(ctx, command, false, "", false)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("shell_command %q: %v", command, err),
			IsError: true,
		}, agenterrors.NewTool("shell_command", fmt.Sprintf("shell_command %q: %v", command, err), err)
	}

	// A command that hit the tool deadline was promoted to a background
	// session mid-run. Attach the same wakeup watcher the explicit
	// background=true path uses, so the agent hears about completion
	// instead of having to remember to poll (wakeup_timeout=0: completion
	// notification only, no deadline heads-up).
	if sessionID, promoted := ParsePromotedBackgroundSession(result); promoted {
		rememberBackgroundCommand(sessionID, command)
		if env.Notifier != nil {
			h.startWakeupWatcher(ctx, env, fmt.Sprintf(`{"session_id":%q,"status":"running"}`, sessionID), 0, command)
		}
	}

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

type bgResult struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

func (h *shellCommandHandler) startWakeupWatcher(ctx context.Context, env ToolEnv, resultJSON string, timeoutSec int, command string) {
	var res bgResult
	if err := json.Unmarshal([]byte(resultJSON), &res); err != nil || res.SessionID == "" {
		return
	}
	sessionID := res.SessionID
	var done <-chan struct{}
	var getExitCode func() int

	notifier := env.Notifier
	if notifier == nil {
		return
	}
	// Short command prefix shown to the user in wakeup bubbles
	// ("Looking into 'make build'…"). The full command stays in the
	// agent-facing content.
	label := shortCommandLabel(command)

	// Cap the deadline so absurd values can't overflow time.Duration
	// (a wrapped-negative duration fires the timer immediately).
	if timeoutSec > maxWakeupTimeoutSeconds {
		timeoutSec = maxWakeupTimeoutSeconds
	}

	// Use the agent's lifetime context for the watcher goroutines so they
	// survive turn boundaries. The per-turn ctx is cancelled when the model
	// finishes its response, which would kill watchers that are waiting for
	// long-running background tasks. LifetimeCtx lives until agent Shutdown.
	watchCtx := env.LifetimeCtx
	if watchCtx == nil {
		watchCtx = context.Background()
	}

	if tm := TerminalManagerFromContext(ctx); tm != nil {
		if doneCh, ok := tm.BackgroundDoneChan(sessionID); ok {
			// Sentinel-equipped session: the channel closes on real command
			// completion (or session death), and the exit code is real.
			done = doneCh
			getExitCode = func() int { return tm.BackgroundExitCode(sessionID) }
		} else {
			// Session unknown (already reaped?) or a pre-sentinel background
			// session. Fall back to liveness polling so the watcher still
			// reports when the session dies.
			liveCh := make(chan struct{})
			done = liveCh
			go func() {
				ticker := time.NewTicker(500 * time.Millisecond)
				defer ticker.Stop()
				for tm.IsSessionActive(sessionID) {
					select {
					case <-ticker.C:
					case <-watchCtx.Done():
						// Cancelled before the session finished: leave liveCh
						// open so the completion goroutine also takes the
						// cancellation branch instead of emitting a spurious
						// completion notification.
						return
					}
				}
				close(liveCh)
			}()
			getExitCode = func() int { return BgExitNone }
		}
	} else if bpm := BackgroundProcessManagerFromContext(ctx); bpm != nil {
		if proc, exists := bpm.GetProcess(sessionID); exists {
			done = proc.Done()
			getExitCode = proc.GetExitCode
		} else {
			return
		}
	} else {
		return
	}

	// Deadline heads-up: fires at most once if the session is still running
	// after timeoutSec seconds. It never stops the completion watch below —
	// the deadline is a heads-up, not a give-up.
	if timeoutSec > 0 {
		go func() {
			timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
				// Completion can win the race with the timer; suppress the
				// heads-up in that case so the message always means "still
				// running" and stays ordered before the completion notice.
				select {
				case <-done:
					return
				default:
				}
				notifier.NotifyCompletionLabeled(sessionID, "shell_bg_timeout",
					fmt.Sprintf("Background session %s still running after %ds (wakeup deadline reached).\nIt will be notified again when it completes.",
						sessionID, timeoutSec), label)
			case <-done:
				// Completed before the deadline; the completion goroutine
				// already reported it — no heads-up needed.
			case <-watchCtx.Done():
			}
		}()
	}

	// Completion watch: exactly one notification when the session exits.
	go func() {
		select {
		case <-done:
			// The background command's writes happened while unattended.
			// This is the first observation point — capture them so the
			// changes panel and recovery reflect reality. Best-effort.
			trackBackgroundMutation(env, sessionID, "completed")
			notifier.NotifyCompletionLabeled(sessionID, "shell_bg",
				formatShellBgCompletion(sessionID, getExitCode(), tailOfSessionOutput(ctx, sessionID)), label)
		case <-watchCtx.Done():
		}
	}()
}
