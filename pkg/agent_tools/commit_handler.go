package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// GenerateCommitMessageFunc is a function pointer set by pkg/agent during
// agent construction (wireAgentToolFuncs). It bridges the interface-based
// commit tool with the sprout commit Conventional-Commit generator
// (pkg/git.GenerateCommitMessageFromStagedDiff) plus the agent's LLM client,
// which the tool layer must not import (import direction: pkg/agent imports
// pkg/agent_tools, never the other way around).
//
// It is called with the full staged diff and the caller's notes (context for
// the generated message) and returns the generated Conventional Commit
// message. Nil in standalone tool runs (no agent constructed yet) — the
// commit handler then refuses to commit rather than guessing a message.
// Guarded by ToolFuncMu like the other package-level function pointers.
var GenerateCommitMessageFunc func(diff []byte, notes string) (string, error)

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
			{Name: "message", Type: "string", Description: "Explicit commit message, used as-is when provided (shell-safe: backticks, $(), and other special characters are not expanded). When omitted, a Conventional Commit message is generated from the staged diff, with notes as context; if generation fails, nothing is committed."},
			{Name: "notes", Type: "string", Description: "Context used to shape the commit message when message is omitted (e.g., 'fixes the flaky auth test'). Ignored when message is provided."},
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

	// message omitted: generate a Conventional Commit message from the
	// staged diff, with notes as context. Never commit a placeholder
	// ("Auto-commit") or the notes verbatim — when generation fails or no
	// generator is wired, commit nothing.
	//
	// The diff is read through pkg/git (the shared git-operations layer)
	// rather than the shell tool: the shell tool wraps command output in a
	// status header, which would defeat the empty-diff check and pollute
	// the generator's prompt.
	stagedDiff, err := git.GetStagedDiff(effectiveDir)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Commit message generation failed, so nothing was committed: could not read the staged diff: %v", err),
			IsError: true,
		}, nil
	}
	if strings.TrimSpace(stagedDiff) == "" {
		return ToolResult{
			Output:  "No staged changes to commit. Stage files first (git add), or pass an explicit message.",
			IsError: true,
		}, nil
	}

	generate := env.ResolveToolFuncs().GenerateCommitMessage
	if generate == nil {
		return ToolResult{
			Output:  "Commit message generation failed, so nothing was committed: no commit message generator is available in this session. Provide an explicit 'message' instead.",
			IsError: true,
		}, nil
	}

	commitMsg, err := generate([]byte(stagedDiff), notes)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Commit message generation failed, so nothing was committed: %v\n\nStaged diff (truncated):\n%s", err, previewStagedDiff(stagedDiff)),
			IsError: true,
		}, nil
	}

	return commitMessage(ctx, commitMsg, effectiveDir)
}

// previewStagedDiff caps a staged diff for display in error results,
// mirroring the truncated diff preview the CLI commit flow shows.
func previewStagedDiff(diff string) string {
	const maxPreviewBytes = 2000
	if len(diff) <= maxPreviewBytes {
		return diff
	}
	return diff[:maxPreviewBytes] + "\n... (truncated)"
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
// extraArgs are further `git commit` flags (e.g. --amend), already in shell
// syntax.
func commitMessage(ctx context.Context, message, workingDir string, extraArgs ...string) (ToolResult, error) {
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

	cmd := "git commit -F " + shellQuotePath(msgFile.Name())
	for _, a := range extraArgs {
		if a = strings.TrimSpace(a); a != "" {
			cmd += " " + a
		}
	}
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

// shellQuotePath renders a local path for a command line run by the shell
// tool. Forward slashes survive Git Bash, which strips unquoted
// backslashes from Windows paths like C:\Users\me\AppData\Local\Temp, and
// git accepts them on every OS. Double quotes keep spaces intact in both
// bash and the cmd.exe fallback; the characters bash still expands inside
// them are escaped.
func shellQuotePath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.NewReplacer(`"`, `\"`, "$", `\$`, "`", "\\`").Replace(p)
	return `"` + p + `"`
}
