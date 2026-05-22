package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type gitHandler struct{}

func (h *gitHandler) Name() string { return "git" }

func (h *gitHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "git",
		Description: "Execute git operations that modify repository state or require network access. All destructive operations require user approval. Commit operations should use the /commit slash command for the interactive commit flow. For read-only operations (status, log, diff, branch, show), use shell_command instead.",
		Required: []string{"operation"},
		Parameters: []ParameterDef{
			{Name: "operation", Type: "string", Required: true, Description: "Git operation type: commit, push, pull, fetch, add, rm, mv, reset, rebase, merge, checkout, branch_delete, tag, clean, stash, am, apply, cherry_pick, revert, restore"},
			{Name: "args", Type: "string", Description: "Arguments to pass to the git command (optional). For pull: --rebase, --ff-only, remote/branch. For fetch: --all, --prune, remote."},
		},
	}
}

func (h *gitHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "operation")
	return err
}

// dangerousOperations require user confirmation before execution
var dangerousOps = map[string]bool{
	"reset":        true,
	"rebase":       true,
	"clean":        true,
	"branch_delete": true,
	"revert":       true,
	"merge":        true,
	"push":         true,
}

func (h *gitHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	operation, _ := extractString(args, "operation")
	argsStr, _ := extractString(args, "args")

	// Validate operation
	validOps := []string{"commit", "push", "pull", "fetch", "add", "rm", "mv", "reset", "rebase", "merge", "checkout", "branch_delete", "tag", "clean", "stash", "am", "apply", "cherry_pick", "revert", "restore"}
	valid := false
	for _, op := range validOps {
		if op == operation {
			valid = true
			break
		}
	}
	if !valid {
		return ToolResult{Output: fmt.Sprintf("Invalid git operation: %q. Valid operations: %s", operation, strings.Join(validOps, ", ")), IsError: true}, nil
	}

	// Map operation to git command
	var gitCmd string
	switch operation {
	case "commit":
		gitCmd = "git commit"
	case "push":
		gitCmd = "git push"
	case "pull":
		gitCmd = "git pull"
	case "fetch":
		gitCmd = "git fetch"
	case "add":
		gitCmd = "git add"
	case "rm":
		gitCmd = "git rm"
	case "mv":
		gitCmd = "git mv"
	case "reset":
		gitCmd = "git reset"
	case "rebase":
		gitCmd = "git rebase"
	case "merge":
		gitCmd = "git merge"
	case "checkout":
		gitCmd = "git checkout"
	case "branch_delete":
		gitCmd = "git branch -D"
	case "tag":
		gitCmd = "git tag"
	case "clean":
		gitCmd = "git clean"
	case "stash":
		gitCmd = "git stash"
	case "am":
		gitCmd = "git am"
	case "apply":
		gitCmd = "git apply"
	case "cherry_pick":
		gitCmd = "git cherry-pick"
	case "revert":
		gitCmd = "git revert"
	case "restore":
		gitCmd = "git restore"
	default:
		return ToolResult{Output: fmt.Sprintf("Unsupported git operation: %s", operation), IsError: true}, nil
	}

	if argsStr != "" {
		gitCmd += " " + argsStr
	}

	// Check if this is a dangerous operation requiring approval
	if dangerousOps[operation] {
		if env.ApprovalManager != nil {
			result := env.ApprovalManager.RequestApproval(
				operation,
				"git",
				"high",
				fmt.Sprintf("Execute dangerous git operation: %s\nCommand: %s", operation, gitCmd),
				nil,
			)
			if !result.Approved {
				reason := "denied"
				if result.Reason != "" {
					reason = result.Reason
				}
				return ToolResult{Output: fmt.Sprintf("Git operation %q was %s by approval manager", operation, reason)}, nil
			}
		} else {
			// No approval manager - warn but proceed
			fmt.Fprintf(os.Stderr, "WARNING: Dangerous git operation %q without approval manager\n", operation)
		}
	}

	result, err := execShellCmd(ctx, gitCmd, env.WorkspaceRoot)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Git operation failed: %v", err), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}
