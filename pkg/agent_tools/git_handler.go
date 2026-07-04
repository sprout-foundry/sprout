package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

type gitHandler struct{}

func (h *gitHandler) Name() string { return "git" }

func (h *gitHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "git",
		Description: "Execute git operations that modify repo state or require network. Destructive ops require approval. For read-only (status, log, diff), use shell_command instead.",
		Required:    []string{"operation"},
		Parameters: []ParameterDef{
			{Name: "operation", Type: "string", Required: true, Description: "Git operation: commit, push, pull, fetch, add, rm, mv, reset, rebase, merge, checkout, branch_delete, tag, clean, stash, am, apply, cherry_pick, revert, restore"},
			{Name: "args", Type: "string", Description: "Args to pass to the git command"},
		},
	}
}

func (h *gitHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "operation")
	return err
}

// dangerousOperations require user confirmation before execution
var dangerousOps = map[string]bool{
	"reset":         true,
	"rebase":        true,
	"clean":         true,
	"branch_delete": true,
	"revert":        true,
	"merge":         true,
	"push":          true,
}

func (h *gitHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// Inject env.WorkspaceRoot into context so downstream code
	// (executeGitCommand, SafeGitCmd, etc.) resolves the correct
	// working directory. Without this, git commands fall back to
	// os.Getwd() which is the package source dir during tests —
	// creating nested .git repos that corrupt the ChangeTracker.
	if env.WorkspaceRoot != "" {
		ctx = filesystem.WithWorkspaceRoot(ctx, env.WorkspaceRoot)
	}

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

	// Normalize args: strip a leading duplicate of the git subcommand.
	// LLMs commonly pass args like "push origin main" when operation is already
	// "push", resulting in "git push push origin main" — a confusing error.
	argsStr = normalizeGitArgs(GitOperationType(operation), argsStr)

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

	// Classify this git operation using the security classifier
	secResult := classifyGitOperation(args)

	// Three-tier approval based on classifier result
	switch secResult.Risk {
	case SecurityDangerous:
		// Destructive operation (e.g., reset --hard, rebase -i)
		if env.ApprovalManager != nil {
			result := env.ApprovalManager.RequestApproval(
				operation,
				"git",
				"critical",
				fmt.Sprintf("⚠️ DESTRUCTIVE git operation: %s\nReason: %s\nCommand: %s", operation, secResult.Reasoning, gitCmd),
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
			// No approval manager - hard-block destructive operations
			return ToolResult{
				Output:  fmt.Sprintf("Blocked: destructive git operation %q (%s). This operation requires interactive approval and cannot proceed without an approval manager.", operation, secResult.Reasoning),
				IsError: true,
			}, nil
		}
	case SecurityCaution:
		// Caution-level operation (e.g., reset --soft, plain reset/rebase)
		// Fall through to legacy dangerousOps map for backward compat
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
		// SecuritySafe: no approval needed, skip entirely
	}

	result, err := execShellCmd(ctx, gitCmd, env.WorkspaceRoot)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Git operation failed: %v", err), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *gitHandler) Aliases() []string         { return nil }
func (h *gitHandler) Timeout() time.Duration    { return 0 }
func (h *gitHandler) MaxResultSize() int        { return 0 }
func (h *gitHandler) SafeForParallel() bool     { return false }
func (h *gitHandler) Interactive() bool         { return false }
// LLMs commonly pass args like "push origin main" when operation is already
// "push", producing "git push push origin main". Handles underscore→hyphen
// conversion (cherry_pick → cherry-pick) and branch_delete → branch.
func normalizeGitArgs(op GitOperationType, args string) string {
	if args == "" {
		return args
	}

	subcommand := string(op)
	if op == GitOpBranchDelete {
		subcommand = "branch"
	} else {
		subcommand = strings.ReplaceAll(subcommand, "_", "-")
	}

	fields := strings.Fields(args)
	if len(fields) > 0 && fields[0] == subcommand {
		return strings.Join(fields[1:], " ")
	}
	return args
}
