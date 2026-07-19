package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
			{Name: "repo_dir", Type: "string", Description: "Subdirectory within the workspace to run git operations in (e.g., for submodules or monorepo workspaces). Must be within the workspace root. Defaults to workspace root if omitted."},
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
	operation, _ := extractString(args, "operation")
	argsStr, _ := extractString(args, "args")
	repoDir, _ := extractString(args, "repo_dir")

	// Determine the effective working directory: repo_dir overrides WorkspaceRoot.
	effectiveDir := env.WorkspaceRoot
	if repoDir != "" {
		resolvedDir, err := validateRepoDir(repoDir, env.WorkspaceRoot)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Invalid repo_dir: %v", err), IsError: true}, nil
		}
		effectiveDir = resolvedDir
	}

	// Inject effective directory into context so downstream code
	// (executeGitCommand, SafeGitCmd, etc.) resolves the correct
	// working directory. Without this, git commands fall back to
	// os.Getwd() which is the package source dir during tests —
	// creating nested .git repos that corrupt the ChangeTracker.
	if effectiveDir != "" {
		ctx = filesystem.WithWorkspaceRoot(ctx, effectiveDir)
	}

	// Validate args against the dangerous-args blocklist before any further
	// processing. This is the same blocklist the legacy ExecuteGitOperation
	// path uses; without it here, the new tool handler accepts flags like
	// --upload-pack=evil.sh or -c core.hooksPath=/tmp/hooks, which are then
	// concatenated into a shell command and executed. Skipping this is a
	// direct command-injection path through LLM-supplied args.
	if err := ValidateGitArgs(argsStr); err != nil {
		return ToolResult{Output: fmt.Sprintf("Blocked git args: %v", err), IsError: true}, nil
	}

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
	useCommitMessage := false
	commitMsg := ""
	switch operation {
	case "commit":
		// Check if args contain -m with a message. If so, extract the message
		// and use the temp-file approach to avoid shell expansion.
		if argsStr != "" {
			remainingArgs, extractedMsg, found := extractCommitMessage(argsStr)
			if found {
				useCommitMessage = true
				commitMsg = extractedMsg
				gitCmd = "git commit -F %s"
				if remainingArgs != "" {
					gitCmd += " " + remainingArgs
				}
			} else {
				gitCmd = "git commit"
			}
		} else {
			gitCmd = "git commit"
		}
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

	// For commit with -m message, use the temp-file approach shared with
	// the commit tool. This prevents shell expansion of backticks, $(), etc.
	if useCommitMessage && commitMsg != "" {
		result, err := commitMessage(ctx, commitMsg, effectiveDir)
		if err != nil {
			return result, err
		}
		return result, nil
	}

	if argsStr != "" && !useCommitMessage {
		gitCmd += " " + argsStr
	}

	// Classify this git operation using the security classifier
	secResult := classifyGitOperation(args)

	// Three-tier approval based on classifier result
	switch secResult.Risk {
	case SecurityDangerous:
		// Destructive operation (e.g., reset --hard, rebase -i)
		if env.SessionElevated && !secResult.IsHardBlock {
			// Elevated session (permissive/unrestricted profile) — Gate 1
			// already auto-approved. Skip the interactive prompt here to
			// match Gate 1's decision and avoid double-prompting.
		} else if env.ApprovalManager != nil {
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
			if env.SessionElevated && !secResult.IsHardBlock {
				// Elevated session — skip approval, matching Gate 1.
			} else if env.ApprovalManager != nil {
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

	result, err := execShellCmd(ctx, gitCmd, effectiveDir)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Git operation failed: %v", err), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *gitHandler) Aliases() []string      { return nil }
func (h *gitHandler) Timeout() time.Duration { return 0 }
func (h *gitHandler) MaxResultSize() int     { return 0 }
func (h *gitHandler) SafeForParallel() bool  { return false }
func (h *gitHandler) Interactive() bool      { return false }

// validateRepoDir validates that repoDir resolves to a path within workspaceRoot.
// Returns the absolute resolved path, or an error if the path is invalid or escapes
// the workspace boundary.
func validateRepoDir(repoDir, workspaceRoot string) (string, error) {
	if workspaceRoot == "" {
		return "", fmt.Errorf("workspace root is not set")
	}

	// Resolve workspace root to absolute path.
	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	// Resolve repoDir: if relative, join to workspace root; if absolute, use as-is.
	cleanDir := filepath.Clean(repoDir)
	var absRepoDir string
	if filepath.IsAbs(cleanDir) {
		absRepoDir = cleanDir
	} else {
		absRepoDir = filepath.Join(absWorkspace, cleanDir)
	}

	// Normalize to absolute.
	absRepoDir, err = filepath.Abs(absRepoDir)
	if err != nil {
		return "", fmt.Errorf("resolve repo_dir: %w", err)
	}

	// Check the resolved path is within the workspace root.
	rel, err := filepath.Rel(absWorkspace, absRepoDir)
	if err != nil {
		return "", fmt.Errorf("compute relative path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("repo_dir %q resolves outside workspace root %q", repoDir, workspaceRoot)
	}

	// Verify the directory exists.
	info, err := os.Stat(absRepoDir)
	if err != nil {
		return "", fmt.Errorf("repo_dir %q does not exist or is not accessible: %w", repoDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repo_dir %q is not a directory", repoDir)
	}

	return absRepoDir, nil
}

// extractCommitMessage extracts -m <message> from git commit args, handling
// double-quoted, single-quoted, and unquoted message values. Returns the
// remaining args (with -m and its value removed) and the extracted message.
//
// Handles these forms:
//
//	-m "double quoted message"    (multi-word, double-quoted)
//	-m 'single quoted message'    (multi-word, single-quoted)
//	-m unquotedword               (single unquoted word)
//	-m"double quoted"             (compact flag+quote)
//	-m'single quoted'             (compact flag+quote)
//	-munquotedword                (compact flag+unquoted)
func extractCommitMessage(args string) (remainingArgs string, message string, found bool) {
	fields := strings.Fields(args)
	var result []string
	i := 0
	for i < len(fields) {
		f := fields[i]
		if f == "-m" || f == "-M" {
			// -m followed by the message in the next field
			i++
			if i >= len(fields) {
				// -m without a value — leave it as-is
				result = append(result, fields[i-1])
				break
			}
			msg, consumed := extractQuotedValue(fields[i:])
			message = msg
			found = true
			i += consumed
		} else if strings.HasPrefix(f, "-m") || strings.HasPrefix(f, "-M") {
			// Compact form: -m"message" or -m'message' or -munquoted
			rest := f[2:]
			if rest != "" {
				msg, _ := stripQuotes(rest)
				message = msg
				found = true
				i++
			} else {
				// -m followed by nothing (edge case)
				result = append(result, f)
				i++
			}
		} else {
			result = append(result, f)
			i++
		}
	}
	return strings.Join(result, " "), message, found
}

// extractQuotedValue extracts a potentially multi-word quoted value from the
// beginning of tokens. Handles double-quoted ("), single-quoted ('), and
// unquoted single-word values. Returns the extracted value and the number of
// tokens consumed.
func extractQuotedValue(tokens []string) (value string, consumed int) {
	if len(tokens) == 0 {
		return "", 0
	}
	first := tokens[0]

	// Single-word unquoted value
	if !strings.HasPrefix(first, "\"") && !strings.HasPrefix(first, "'") {
		return first, 1
	}

	// Quoted value — may span multiple tokens
	quote := first[0]
	var parts []string
	for j, tok := range tokens {
		if j == 0 {
			// First token — strip leading quote
			rest := tok[1:]
			if strings.HasSuffix(rest, string(quote)) {
				// Complete in one token: "message"
				return rest[:len(rest)-1], 1
			}
			parts = append(parts, rest)
		} else {
			if strings.HasSuffix(tok, string(quote)) {
				// Closing quote found
				parts = append(parts, tok[:len(tok)-1])
				return strings.Join(parts, " "), j + 1
			}
			parts = append(parts, tok)
		}
	}
	// No closing quote found — return everything as-is
	return strings.Join(parts, " "), len(tokens)
}

// stripQuotes removes matching surrounding quotes from a string.
func stripQuotes(s string) (string, bool) {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1], true
		}
	}
	return s, false
}

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
