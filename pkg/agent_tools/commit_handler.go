package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type commitHandler struct{}

func (h *commitHandler) Name() string { return "commit" }

func (h *commitHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "commit",
		Description: "Commit staged changes with an auto-generated commit message. Use this tool instead of running 'git commit' directly. This tool uses the commit message generation and validation system. For read-only operations like status, log, diff, use shell_command instead.",
		Required: []string{},
		Parameters: []ParameterDef{
			{Name: "message", Type: "string", Description: "Commit message (optional). If not provided, a message will be auto-generated based on the staged changes."},
			{Name: "notes", Type: "string", Description: "Optional notes/context to integrate into the auto-generated commit message. Use this to provide context about why the changes were made, what task they relate to, or any other information that should be captured in the commit. These notes are combined with the diff analysis to produce a better commit message. Ignored if 'message' parameter is provided."},
		},
	}
}

func (h *commitHandler) Validate(args map[string]any) error {
	if args == nil || len(args) == 0 {
		return fmt.Errorf("arguments must not be nil or empty")
	}
	return nil
}

func (h *commitHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	message, _ := extractString(args, "message")
	notes, _ := extractString(args, "notes")

	if message != "" {
		// Use the provided message
		cmd := fmt.Sprintf("git commit -m %q", message)
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

	cmd := fmt.Sprintf("git commit -m %q", msg)
	output, err := execShellCmd(ctx, cmd, env.WorkspaceRoot)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Commit failed: %v\n\nStaged changes were:\n%s", err, result), IsError: true}, nil
	}
	return ToolResult{Output: output}, nil
}

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
