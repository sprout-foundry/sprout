package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

type commitHandler struct{}

func (h *commitHandler) Name() string { return "commit" }

func (h *commitHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "commit",
		Description: "Commit staged changes with an auto-generated or custom message. Use this instead of 'git commit' directly. " +
			"The message body is shell-safe (backticks, $(), and special chars in the message are not expanded). " +
			"For read-only git operations (status, log, diff), use shell_command.",
		Required: []string{},
		Parameters: []ParameterDef{
			{Name: "message", Type: "string", Description: "Commit message (auto-generated if omitted). Shell-safe: backticks, $(), and other special characters are not expanded."},
			{Name: "notes", Type: "string", Description: "Context for auto-generated message (ignored if message is provided)"},
		},
	}
}

func (h *commitHandler) Validate(args map[string]any) error {
	if args == nil || len(args) == 0 {
		return agenterrors.NewValidation("arguments must not be nil or empty", nil)
	}
	return nil
}

func (h *commitHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	message, _ := extractString(args, "message")
	notes, _ := extractString(args, "notes")

	if message != "" {
		// Write message to a temp file to avoid shell expansion of
		// backticks, $(), ", and other special characters. The file
		// path passed to git commit -F is safe in any shell context.
		msgFile, err := os.CreateTemp("", "sprout-commit-msg-*")
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Commit failed: %v", err), IsError: true}, nil
		}
		defer os.Remove(msgFile.Name())
		if _, err := msgFile.WriteString(message); err != nil {
			msgFile.Close()
			return ToolResult{Output: fmt.Sprintf("Commit failed: %v", err), IsError: true}, nil
		}
		msgFile.Close()

		cmd := fmt.Sprintf("git commit -F %s", msgFile.Name())
		result, err := execShellCmd(ctx, cmd, env.WorkspaceRoot)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Commit failed: %v", err), IsError: true}, nil
		}
		return ToolResult{Output: result}, nil
	}

	// Auto-generate from diff + notes
	result, err := execShellCmd(ctx, "git diff --cached --stat", env.WorkspaceRoot)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to read staged changes: %v", err), IsError: true}, nil
	}

	// Build a simple auto-generated message
	msg := "Auto-commit"
	if notes != "" {
		msg = notes
	}

	msgFile, err := os.CreateTemp("", "sprout-commit-msg-*")
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Commit failed: %v", err), IsError: true}, nil
	}
	defer os.Remove(msgFile.Name())
	if _, err := msgFile.WriteString(msg); err != nil {
		msgFile.Close()
		return ToolResult{Output: fmt.Sprintf("Commit failed: %v", err), IsError: true}, nil
	}
	msgFile.Close()

	cmd := fmt.Sprintf("git commit -F %s", msgFile.Name())
	output, err := execShellCmd(ctx, cmd, env.WorkspaceRoot)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Commit failed: %v\n\nStaged changes were:\n%s", err, result), IsError: true}, nil
	}
	return ToolResult{Output: output}, nil
}

func (h *commitHandler) Aliases() []string      { return nil }
func (h *commitHandler) Timeout() time.Duration { return 0 }
func (h *commitHandler) MaxResultSize() int     { return 0 }
func (h *commitHandler) SafeForParallel() bool  { return false }
func (h *commitHandler) Interactive() bool      { return false }

// execShellCmd runs a shell command and returns its output
func execShellCmd(ctx context.Context, cmd string, workingDir string) (string, error) {
	sc := &shellCommandHandler{}
	args := map[string]any{"command": cmd}
	if workingDir != "" {
		envCopy := ToolEnv{WorkspaceRoot: workingDir}
		result, err := sc.Execute(ctx, envCopy, args)
		if err != nil {
			return "", err
		}
		return result.Output, nil
	}
	result, err := sc.Execute(ctx, ToolEnv{}, args)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}
