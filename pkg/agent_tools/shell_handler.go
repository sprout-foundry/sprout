package tools

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
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
		Description: "Execute a shell command. Supports background execution (background=true), checking accumulated output of a background session (check_background=session_id, optionally with wait_seconds to block until exit), and stopping a background session (stop_background=session_id).",
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
		},
	}
}

func (h *shellCommandHandler) Validate(args map[string]any) error {
	if args == nil {
		return fmt.Errorf("arguments must not be nil")
	}

	// Extract parameters
	var command string
	if cmdRaw, ok := args["command"]; ok && cmdRaw != nil {
		cmd, err := extractString(args, "command")
		if err != nil {
			return fmt.Errorf("parameter 'command' must be a string")
		}
		command = cmd
	}

	var checkBackground string
	if cbRaw, ok := args["check_background"]; ok && cbRaw != nil {
		cb, err := extractString(args, "check_background")
		if err != nil {
			return fmt.Errorf("parameter 'check_background' must be a string")
		}
		checkBackground = cb
	}

	var stopBackground string
	if sbRaw, ok := args["stop_background"]; ok && sbRaw != nil {
		sb, err := extractString(args, "stop_background")
		if err != nil {
			return fmt.Errorf("parameter 'stop_background' must be a string")
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
			return fmt.Errorf("parameter 'background' must be a boolean")
		}
	}

	// Reject conflicting parameters
	if checkBackground != "" && getBoolArg(args, "background") {
		return fmt.Errorf("check_background and background=true cannot be used together")
	}
	if stopBackground != "" && getBoolArg(args, "background") {
		return fmt.Errorf("stop_background and background=true cannot be used together")
	}
	if stopBackground != "" && checkBackground != "" {
		return fmt.Errorf("stop_background and check_background cannot be used together")
	}

	// wait_seconds is only meaningful with check_background.
	if waitRaw, ok := args["wait_seconds"]; ok && waitRaw != nil {
		wait, err := extractInt(args, "wait_seconds")
		if err != nil {
			return err
		}
		if wait < 0 {
			return fmt.Errorf("parameter 'wait_seconds' must be >= 0")
		}
		if checkBackground == "" && wait > 0 {
			return fmt.Errorf("wait_seconds is only valid with check_background")
		}
	}

	// If neither check_background nor stop_background is set, command is required
	if checkBackground == "" && stopBackground == "" && strings.TrimSpace(command) == "" {
		return fmt.Errorf("command parameter is required when check_background and stop_background are not provided")
	}

	return nil
}

func (h *shellCommandHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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
		}, fmt.Errorf("standalone sleep/wait not supported as a tool call — use check_background with wait_seconds instead")
	}

	// --- Security classification ---
	secResult := ClassifyToolCall("shell_command", args)

	if secResult.ShouldBlock {
		return ToolResult{
			Output:  fmt.Sprintf("security block: shell_command — %s", secResult.Reasoning),
			IsError: true,
		}, fmt.Errorf("security block: shell_command — %s", secResult.Reasoning)
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
			}, fmt.Errorf("shell_command rejected (%s): %s", reason, secResult.Reasoning)
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
			fmt.Errorf("command parameter is required")
	}

	// background mode
	if background {
		return h.handleBackground(ctx, env, command)
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
		}, fmt.Errorf("check background %q: %w", sessionID, err)
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
			}, fmt.Errorf("stop background %q: %w", sessionID, err)
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
			Output:  "stop_background requires WebUI terminal manager or CLI background process manager",
			IsError: true,
		}, fmt.Errorf("stop_background requires WebUI terminal manager or CLI background process manager")
	}

	err := bpm.Stop(sessionID, 10*time.Second)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("stop background %q: %v", sessionID, err),
			IsError: true,
		}, fmt.Errorf("stop background %q: %w", sessionID, err)
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
		}, fmt.Errorf("execute background command: %w", err)
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
	// Publish tool start event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":    "shell_command",
			"command": truncateForEvent(command, 200),
		})
	}

	// Execute with safety checks.
	// interactiveMode=false, streamOutput=false for agent tool calls.
	result, err := ExecuteShellCommandWithSafety(ctx, command, false, "", false)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("shell_command %q: %v", command, err),
			IsError: true,
		}, fmt.Errorf("shell_command %q: %w", command, err)
	}

	// Publish tool end event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
			"tool":    "shell_command",
			"command": truncateForEvent(command, 200),
			"bytes":   len(result),
			"tokens":  estimateTokenUsage(result),
		})
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

// truncateForEvent truncates a string for event logging.
func truncateForEvent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
