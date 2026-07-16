package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// shellCommandHandler implements ToolHandler for the shell_command tool.
//
// This is the most complex handler. It delegates to existing functions in
// shell.go for actual execution but adds the ToolHandler lifecycle (Name,
// Definition, Validate, security classification + approval).
//
// IMPORTANT: The handler is kept thin — it does NOT do git-specific blocking
// (isGitCheckoutSubcommand, isGitDiscardCommand, etc.) since those require
// *Agent. Those checks remain in the legacy dispatch path (tool_definitions.go).
type shellCommandHandler struct{}

func (h *shellCommandHandler) Name() string {
	return "shell_command"
}

func (h *shellCommandHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "shell_command",
		Description: "Execute a shell command. Supports background execution (background=true), checking accumulated output of a background session (check_background=session_id, optionally with wait_seconds to block until exit), and stopping a background session (stop_background=session_id). Background operations work in CLI as well as WebUI; promoted sessions are discoverable via `sprout shell-bg list`.",
		Parameters: []ParameterDef{
			{
				Name:        "command",
				Type:        "string",
				Required:    false,
				Description: "The shell command to execute (required unless check_background or stop_background is provided)",
			},
			{
				Name:        "background",
				Type:        "boolean",
				Required:    false,
				Description: "Run command in background and return immediately with session_id (default: false)",
			},
			{
				Name:        "check_background",
				Type:        "string",
				Required:    false,
				Description: "Session ID of a background session to check (returns accumulated output)",
			},
			{
				Name:        "wait_seconds",
				Type:        "integer",
				Required:    false,
				Description: "Only valid with check_background. Block (up to this many seconds, max 600) until the session exits, then return the snapshot. Use this for long-running workflows to avoid burning tokens on rapid polling. 0 (default) returns immediately as before.",
			},
			{
				Name:        "stop_background",
				Type:        "string",
				Required:    false,
				Description: "Session ID of a background session to stop/terminate",
			},
			{
				Name:        "wakeup_timeout",
				Type:        "integer",
				Required:    false,
				Description: "Optional deadline in seconds for background commands. The agent is always notified on completion; this adds a timeout notification if the process hasn't finished.",
			},
		},
	}
}

func (h *shellCommandHandler) Validate(args map[string]any) error {
	if args == nil {
		return agenterrors.NewValidation("arguments must not be nil", nil)
	}

	// Extract parameters
	var command string
	if cmdRaw, ok := args["command"]; ok && cmdRaw != nil {
		cmd, err := extractString(args, "command")
		if err != nil {
			return agenterrors.NewValidation("parameter 'command' must be a string", nil)
		}
		command = cmd
	}

	var checkBackground string
	if cbRaw, ok := args["check_background"]; ok && cbRaw != nil {
		cb, err := extractString(args, "check_background")
		if err != nil {
			return agenterrors.NewValidation("parameter 'check_background' must be a string", nil)
		}
		checkBackground = cb
	}

	var stopBackground string
	if sbRaw, ok := args["stop_background"]; ok && sbRaw != nil {
		sb, err := extractString(args, "stop_background")
		if err != nil {
			return agenterrors.NewValidation("parameter 'stop_background' must be a string", nil)
		}
		stopBackground = sb
	}

	// Validate background parameter if provided
	if bgRaw, exists := args["background"]; exists && bgRaw != nil {
		switch bgRaw.(type) {
		case bool:
			// Valid
		case string:
			// String "true"/"false" is acceptable from JSON
		default:
			return agenterrors.NewValidation("parameter 'background' must be a boolean", nil)
		}
	}

	// Reject conflicting parameters
	if checkBackground != "" && getBoolArg(args, "background") {
		return agenterrors.NewValidation("check_background and background=true cannot be used together", nil)
	}
	if stopBackground != "" && getBoolArg(args, "background") {
		return agenterrors.NewValidation("stop_background and background=true cannot be used together", nil)
	}
	if stopBackground != "" && checkBackground != "" {
		return agenterrors.NewValidation("stop_background and check_background cannot be used together", nil)
	}

	// wait_seconds is only meaningful with check_background.
	if waitRaw, ok := args["wait_seconds"]; ok && waitRaw != nil {
		wait, err := extractInt(args, "wait_seconds")
		if err != nil {
			return err
		}
		if wait < 0 {
			return agenterrors.NewValidation("parameter 'wait_seconds' must be >= 0", nil)
		}
		if checkBackground == "" && wait > 0 {
			return agenterrors.NewValidation("wait_seconds is only valid with check_background", nil)
		}
	}

	// wakeup_timeout is only valid with background=true.
	if wtRaw, ok := args["wakeup_timeout"]; ok && wtRaw != nil {
		wt, err := extractInt(args, "wakeup_timeout")
		if err != nil {
			return err
		}
		if wt < 0 {
			return agenterrors.NewValidation("parameter 'wakeup_timeout' must be >= 0", nil)
		}
		if !getBoolArg(args, "background") {
			return agenterrors.NewValidation("wakeup_timeout is only valid with background=true", nil)
		}
	}

	// If neither check_background nor stop_background is set, command is required
	if checkBackground == "" && stopBackground == "" && strings.TrimSpace(command) == "" {
		return agenterrors.NewValidation("command parameter is required when check_background and stop_background are not provided", nil)
	}

	return nil
}

func (h *shellCommandHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// Inject env.WorkspaceRoot into context so runShellCommand resolves
	// the correct cmd.Dir. Without this, the shell falls back to
	// os.Getwd() which is the package source dir during tests —
	// creating nested .git repos that corrupt the ChangeTracker.
	if env.WorkspaceRoot != "" {
		ctx = filesystem.WithWorkspaceRoot(ctx, env.WorkspaceRoot)
	}

	// Extract parameters
	var command string
	if cmdRaw, ok := args["command"]; ok && cmdRaw != nil {
		var err error
		command, err = extractString(args, "command")
		if err != nil {
			return ToolResult{Output: "parameter 'command' must be a string", IsError: true}, err
		}
	}

	var checkBackground string
	if cbRaw, ok := args["check_background"]; ok && cbRaw != nil {
		var err error
		checkBackground, err = extractString(args, "check_background")
		if err != nil {
			return ToolResult{Output: "parameter 'check_background' must be a string", IsError: true}, err
		}
	}

	var stopBackground string
	if sbRaw, ok := args["stop_background"]; ok && sbRaw != nil {
		var err error
		stopBackground, err = extractString(args, "stop_background")
		if err != nil {
			return ToolResult{Output: "parameter 'stop_background' must be a string", IsError: true}, err
		}
	}

	background := getBoolArg(args, "background")

	// Validate: command is required when not doing a background session operation.
	// This catches malformed tool calls early — they are validation failures,
	// not security issues, and should never reach the approval flow.
	if command == "" && checkBackground == "" && stopBackground == "" {
		return ToolResult{
			Output:  "command parameter is required when check_background and stop_background are not provided",
			IsError: true,
		}, agenterrors.NewValidation("command parameter is required when check_background and stop_background are not provided", nil)
	}

	// --- Usage guidance (not a security gate) ---
	// Standalone sleep/wait is an antipattern in tool calls. The classifier
	// returns SecuritySafe for these so no security elevation triggers, but
	// we still return a helpful error so the model knows the right API.
	if isStandaloneSleepOrWaitCommand(command) {
		return ToolResult{
			Output: "Standalone sleep/wait is not appropriate as a shell_command tool call. " +
				"For waiting on a background session, use shell_command(check_background=\"<session_id>\", wait_seconds=<seconds>) — that blocks (up to 10 min) without burning tokens on retries. " +
				"For inserting a delay between commands inside a script, chain with && (e.g., \"cmd1 && sleep 5 && cmd2\"). " +
				"Standalone sleep here will be cut off at the 2-minute shell deadline and adopted as a background session; the agent will NOT have actually waited the requested duration.",
			IsError: true,
		}, agenterrors.NewTool("shell_command", "standalone sleep/wait not supported as a tool call — use check_background with wait_seconds instead", nil)
	}

	// --- Security classification ---
	secResult := ClassifyToolCall("shell_command", args)

	if secResult.ShouldBlock {
		return ToolResult{
			Output:  fmt.Sprintf("security block: shell_command — %s", secResult.Reasoning),
			IsError: true,
		}, agenterrors.NewPermission(fmt.Sprintf("security block: shell_command — %s", secResult.Reasoning), nil)
	}

	if secResult.ShouldPrompt && env.ApprovalManager != nil {
		result := env.ApprovalManager.RequestApproval(
			"", "shell_command", secResult.Risk.String(),
			fmt.Sprintf("Execute shell command: %s\n\n%s", command, secResult.Reasoning),
			nil,
		)
		if !result.Approved {
			reason := result.Reason
			if reason == "" {
				reason = "rejected"
			}
			return ToolResult{
				Output:  fmt.Sprintf("shell_command rejected (%s): %s", reason, secResult.Reasoning),
				IsError: true,
			}, agenterrors.NewPermission(fmt.Sprintf("shell_command rejected (%s): %s", reason, secResult.Reasoning), nil)
		}
	}
	// If ShouldPrompt but ApprovalManager is nil, proceed without approval
	// (WASM/non-interactive case — the static classifier handles hard blocks)

	// --- Dispatch based on operation type ---

	// check_background: retrieve output for a background session
	if checkBackground != "" {
		waitSeconds, err := extractInt(args, "wait_seconds")
		if err != nil {
			return ToolResult{Output: err.Error(), IsError: true}, err
		}
		return h.handleCheckBackground(ctx, env, checkBackground, waitSeconds)
	}

	// stop_background: terminate a background session
	if stopBackground != "" {
		return h.handleStopBackground(ctx, env, stopBackground)
	}

	// command is required for both background and normal execution
	if strings.TrimSpace(command) == "" {
		return ToolResult{Output: "command parameter is required", IsError: true},
			agenterrors.NewValidation("command parameter is required", nil)
	}

	// background mode
	if background {
		wakeupTimeout, _ := extractInt(args, "wakeup_timeout")
		result, err := h.handleBackground(ctx, env, command)
		if err == nil && env.Notifier != nil {
			h.startWakeupWatcher(ctx, env, result.Output, wakeupTimeout)
		}
		return result, err
	}

	// Normal synchronous execution
	return h.handleSync(ctx, env, command)
}

// handleCheckBackground retrieves accumulated output for a background session.
// When waitSeconds > 0, it blocks (capped at maxBackgroundWaitSeconds) until
// the session exits or the wait elapses, then returns the snapshot.
func (h *shellCommandHandler) handleCheckBackground(ctx context.Context, env ToolEnv, sessionID string, waitSeconds int) (ToolResult, error) {
	result, err := CheckBackgroundOutputWait(ctx, sessionID, waitSeconds)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("check background %q: %v", sessionID, err),
			IsError: true,
		}, agenterrors.NewTool("shell_command", fmt.Sprintf("check background %q: %v", sessionID, err), err)
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

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

func (h *shellCommandHandler) Aliases() []string      { return nil }
func (h *shellCommandHandler) Timeout() time.Duration { return 0 }
func (h *shellCommandHandler) MaxResultSize() int     { return 0 }
func (h *shellCommandHandler) SafeForParallel() bool  { return false }
func (h *shellCommandHandler) Interactive() bool      { return false }

// truncateForEvent truncates a string for event logging.
func truncateForEvent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

type bgResult struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

func (h *shellCommandHandler) startWakeupWatcher(ctx context.Context, env ToolEnv, resultJSON string, timeoutSec int) {
	var res bgResult
	if err := json.Unmarshal([]byte(resultJSON), &res); err != nil || res.SessionID == "" {
		return
	}
	sessionID := res.SessionID
	var done <-chan struct{}
	var getExitCode func() int

	if tm := TerminalManagerFromContext(ctx); tm != nil {
		doneCh := make(chan struct{})
		done = doneCh
		exitCh := make(chan int, 1)
		go func() {
			defer close(doneCh)
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			var deadline time.Time
			hasDeadline := timeoutSec > 0
			if hasDeadline {
				deadline = time.Now().Add(time.Duration(timeoutSec) * time.Second)
			}
			for {
				if !tm.IsSessionActive(sessionID) {
					exitCh <- 0
					return
				}
				if hasDeadline && time.Now().After(deadline) {
					exitCh <- -1
					return
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					return
				}
			}
		}()
		getExitCode = func() int { return <-exitCh }
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

	go func() {
		select {
		case <-done:
			exitCode := getExitCode()
			if exitCode == -1 {
				env.Notifier.NotifyCompletion(sessionID, "shell_bg_timeout",
					fmt.Sprintf("Timed out waiting for background session %s after %ds.\nUse shell_command(check_background=%q) to check status.",
						sessionID, timeoutSec, sessionID))
			} else {
				env.Notifier.NotifyCompletion(sessionID, "shell_bg",
					fmt.Sprintf("Background session %s completed with exit code %d.\nUse shell_command(check_background=%q) to see output.",
						sessionID, exitCode, sessionID))
			}
		case <-ctx.Done():
		}
	}()
}
