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
			{Name: "repo_dir", Type: "string", Description: "Subdirectory within the workspace to commit in (e.g., for submodules or monorepo workspaces). Must be within the workspace root. Defaults to workspace root if omitted."},
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
	repoDir, _ := extractString(args, "repo_dir")

	// Determine the effective working directory.
	effectiveDir := env.WorkspaceRoot
	if repoDir != "" {
		resolvedDir, err := validateRepoDir(repoDir, env.WorkspaceRoot)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Invalid repo_dir: %v", err), IsError: true}, nil
		}
		effectiveDir = resolvedDir
	}

	if message != "" {
		return commitMessage(ctx, message, effectiveDir)
	}

	// Auto-generate from diff + notes
	stagedResult, err := execShellCmd(ctx, "git diff --cached --stat", effectiveDir)
	if err != nil {
		stagedResult = "(could not read staged changes)"
	}

	// Build a simple auto-generated message
	msg := "Auto-commit"
	if notes != "" {
		msg = notes
	}

	result, err := commitMessage(ctx, msg, effectiveDir)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Commit failed: %v\n\nStaged changes were:\n%s", err, stagedResult), IsError: true}, nil
	}
	return result, nil
}

func (h *commitHandler) Aliases() []string      { return nil }
func (h *commitHandler) Timeout() time.Duration { return 0 }
func (h *commitHandler) MaxResultSize() int     { return 0 }
func (h *commitHandler) SafeForParallel() bool  { return false }
func (h *commitHandler) Interactive() bool      { return false }

// commitMessage writes a message to a temp file and runs git commit -F.
// Using a temp file avoids shell expansion of backticks, $(), and other
// special characters that would be interpreted passing -m through the shell.
//
// This is shared by both the commit tool and the git tool's commit operation.
func commitMessage(ctx context.Context, message, workingDir string) (ToolResult, error) {
	msgFile, err := os.CreateTemp("", "sprout-commit-msg-*")
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Commit failed: %v", err), IsError: true}, nil
	}
	// Clean up temp file after the shell command completes.
	// On success git deletes its reference; on failure the file is harmless.
	defer os.Remove(msgFile.Name())
	if _, err := msgFile.WriteString(message); err != nil {
		msgFile.Close()
		return ToolResult{Output: fmt.Sprintf("Commit failed: %v", err), IsError: true}, nil
	}
	msgFile.Close()

	cmd := fmt.Sprintf("git commit -F %s", msgFile.Name())
	output, err := execShellCmd(ctx, cmd, workingDir)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Commit failed: %v", err), IsError: true}, nil
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
