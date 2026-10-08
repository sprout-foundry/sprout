package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
		Name: "shell_command",
		Description: "Execute a shell command. Supports background execution (background=true), checking accumulated output of a background session (check_background=session_id, optionally with wait_seconds to block until exit), and stopping a background session (stop_background=session_id). Background operations work in CLI as well as WebUI; promoted sessions are discoverable via `sprout shell-bg list`. The browser shell itself is a POSIX-style shell (pipes, redirections, heredocs, if/for/while/case, functions, $(...), $((...)), set -e) over built-in tools: ls, cat, cp, mv, rm, mkdir, touch, find, grep, rg, sed, awk, jq, sort, uniq, cut, tr, head, tail, wc, diff, xargs, printf, echo, test, base64, sha256sum, stat, du. Compilers, package managers, interpreters (node, python, go) and network tools are not available there; such a command either runs in the user's cloud workspace — if they approve — and returns its real output, or exits 127 with a note explaining why it didn't run; don't retry it in a loop." +
			" In browser/wasm environments, git and GitHub run in-browser: the `git` command answers the read-only subcommands (status, diff, log, show, branch, remote, ls-files, rev-list, rev-parse, symbolic-ref, ...) AND the local/remote write subcommands (add, commit, checkout, switch, fetch, push, pull, clone, init, rm, mv); the `gh` command answers `repo clone`, `repo view`, `pr list`, `pr view`, `pr checkout`, `pr create`, `pr diff`, `pr status`, and `auth status`. For structured git operations you can also call `gittool:<tool> <json>` — available tools: git_clone {url, branch?, depth?}, git_list_repos {}, git_status {repo}, git_diff {repo}, git_log {repo, depth?}, git_branch_list {repo}, git_refs {repo, remote?}, git_read_file {repo, filepath}, git_write_file {repo, filepath, content}, git_list_files {repo}, git_add {repo, filepath?}, git_commit {repo, message}, git_fetch {repo, remote?, ref?, branch?}, git_create_branch {repo, name}, git_config {repo, key, value?}, git_checkout {repo, ref}, git_pull {repo, branch?}, git_push {repo, branch?} (remote ops need a GitHub token; public clone does not). Check out a PR head with `gh pr checkout <n>`, which fetches and checks out its branch in one step." + shellPlatformNote(),
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
				Description: "Optional deadline in seconds for background commands. When the background command completes, the agent is automatically notified and resumed so it can check the output. This adds an early timeout notification if the process hasn't finished within the deadline. The watcher survives turn boundaries — the notification will fire even if the agent's current turn has already ended.",
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
	if cmdRaw, ok := lookupKey(args, "command"); ok && cmdRaw != nil {
		cmd, err := extractString(args, "command")
		if err != nil {
			return agenterrors.NewValidation("parameter 'command' must be a string", nil)
		}
		command = cmd
	}

	var checkBackground string
	if cbRaw, ok := lookupKey(args, "check_background"); ok && cbRaw != nil {
		cb, err := extractString(args, "check_background")
		if err != nil {
			return agenterrors.NewValidation("parameter 'check_background' must be a string", nil)
		}
		checkBackground = cb
	}

	var stopBackground string
	if sbRaw, ok := lookupKey(args, "stop_background"); ok && sbRaw != nil {
		sb, err := extractString(args, "stop_background")
		if err != nil {
			return agenterrors.NewValidation("parameter 'stop_background' must be a string", nil)
		}
		stopBackground = sb
	}

	// Validate background parameter if provided
	if bgRaw, exists := lookupKey(args, "background"); exists && bgRaw != nil {
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
	if waitRaw, ok := lookupKey(args, "wait_seconds"); ok && waitRaw != nil {
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
	if wtRaw, ok := lookupKey(args, "wakeup_timeout"); ok && wtRaw != nil {
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

	// Scope hidden/background shell sessions to this conversation so
	// multi-chat daemons don't share one PTY (cwd pollution) and the
	// per-chat background cap is actually per chat.
	if env.ChatID != "" {
		ctx = WithShellChatID(ctx, env.ChatID)
	}

	// Extract parameters
	var command string
	if cmdRaw, ok := lookupKey(args, "command"); ok && cmdRaw != nil {
		var err error
		command, err = extractString(args, "command")
		if err != nil {
			return ToolResult{Output: "parameter 'command' must be a string", IsError: true}, err
		}
	}

	var checkBackground string
	if cbRaw, ok := lookupKey(args, "check_background"); ok && cbRaw != nil {
		var err error
		checkBackground, err = extractString(args, "check_background")
		if err != nil {
			return ToolResult{Output: "parameter 'check_background' must be a string", IsError: true}, err
		}
	}

	var stopBackground string
	if sbRaw, ok := lookupKey(args, "stop_background"); ok && sbRaw != nil {
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
	secResult := ClassifyToolCallWithWorkspace("shell_command", args, env.WorkspaceRoot)

	// Only truly unrecoverable operations (IsHardBlock) are blocked outright.
	// ShouldBlock without IsHardBlock falls through to the approval prompt
	// below — the user can still approve or reject it interactively.
	if secResult.IsHardBlock {
		return ToolResult{
			Output:  fmt.Sprintf("security block: shell_command — %s", secResult.Reasoning),
			IsError: true,
		}, agenterrors.NewPermission(fmt.Sprintf("security block: shell_command — %s", secResult.Reasoning), nil)
	}

	// A prompt is skipped when Gate 1 already auto-approved the call
	// (--unsafe mode or session elevation) or when the session opted into
	// --unsafe-shell and this command is not excluded from that bypass.
	// Hard blocks and DANGEROUS commands stay gated: --unsafe-shell matches
	// the broker's CAUTION-tier bypass in practice (non-hard-block,
	// non-DANGEROUS, no intent confirmation), only lifting the CAUTION-tier
	// shell prompt.
	shellAutoApproved := env.Gate1AutoApproved && !secResult.IsHardBlock
	if !shellAutoApproved && env.UnsafeShellMode && !secResult.IsHardBlock &&
		secResult.Risk.String() != "DANGEROUS" && !secResult.IntentConfirmation {
		shellAutoApproved = true
	}
	if (secResult.ShouldPrompt || secResult.ShouldBlock) && env.ApprovalManager != nil && !shellAutoApproved {
		approvalExtras := map[string]string{}
		if command != "" {
			approvalExtras["command"] = command
		}
		result := env.ApprovalManager.RequestApproval(
			"", "shell_command", secResult.Risk.String(),
			fmt.Sprintf("Execute shell command: %s\n\n%s", command, secResult.Reasoning),
			approvalExtras,
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

	// Establish the change-tracking baseline before the command can mutate
	// anything (a no-op once primed, and for read-only commands).
	prepareShellMutation(env, command)

	// background mode
	if background {
		wakeupTimeout, _ := extractInt(args, "wakeup_timeout")
		result, err := h.handleBackground(ctx, env, command)
		if err == nil {
			// Remember the session→command mapping so ANY completion
			// observer (wakeup watcher, check_background, stop) can
			// classify destructive commands at mutation-diff time.
			var started bgResult
			if json.Unmarshal([]byte(result.Output), &started) == nil && started.SessionID != "" {
				rememberBackgroundCommand(started.SessionID, command)
			}
			if env.Notifier != nil {
				h.startWakeupWatcher(ctx, env, result.Output, wakeupTimeout, command)
			}
		}
		// No mutation diff here: the command has only been STARTED, so a
		// diff would capture nothing and rebase the tracker's baseline
		// to pre-command state — the command's eventual writes would go
		// untracked (or be misattributed to the next shell command).
		// Mutations are recorded when completion is observed, in
		// handleCheckBackground and the wakeup watcher.
		return result, err
	}

	// Normal synchronous execution
	result, syncErr := h.handleSync(ctx, env, command)
	// Post-side tracking (even when the command errored — a partial run may
	// still have written something worth recording). Mirrors the legacy
	// executeShellCommandWithTruncation behavior.
	trackShellMutation(env, command)
	return result, syncErr
}

func (h *shellCommandHandler) Aliases() []string { return nil }

func (h *shellCommandHandler) Timeout() time.Duration { return 0 }

func (h *shellCommandHandler) MaxResultSize() int { return 0 }

func (h *shellCommandHandler) SafeForParallel() bool { return false }

func (h *shellCommandHandler) Interactive() bool { return false }

// prepareShellMutation lets the agent's ChangeTracker snapshot the workspace
// before a command runs, so the command's own mutations are diffable. Without
// it, a tracker that skipped the eager snapshot (subagents) took its first
// snapshot after the first mutating command — making that command's changes
// part of the baseline, never recorded and never revertible.
func prepareShellMutation(env ToolEnv, command string) {
	if command == "" {
		return
	}
	if fn := env.ResolveToolFuncs().PrepareShellCommand; fn != nil {
		fn(command)
	}
}
