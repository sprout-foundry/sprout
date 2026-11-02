package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Task worktrees live inside the workspace so they are easy to find and
// survive state-dir cleanup: <workspace>/.sprout/tasks/<slug>.
const taskDirName = "tasks"

// taskBranchMaxLen bounds the branch name the LLM proposes. Long names are
// truncated at a word boundary rather than mid-word.
const taskBranchMaxLen = 48

// taskConventionsBudget caps how much of AGENTS.md goes into the naming
// prompt. The whole file is a few KB; the cap only guards against a
// workspace that has grown a book.
const taskConventionsBudget = 4000

// taskBranchPattern is the shape a task branch must have: a Conventional
// Commit type, a slash, and a lowercase kebab slug.
var taskBranchPattern = regexp.MustCompile(`^(feat|fix|refactor|chore|docs|test|perf|build|ci)/[a-z0-9]+(-[a-z0-9]+)*$`)

// taskSlugSanitizer collapses anything that is not a lowercase alnum into a
// single dash, so a fallback slug is always valid.
var taskSlugSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

// taskWorktree describes a created task worktree.
type taskWorktree struct {
	RepoRoot  string // repository top level
	Path      string // worktree top level
	Branch    string
	Base      string // branch or commit the worktree was created from
	Workspace string // the primary workspace root this task belongs to
}

// taskBaseDir returns the directory holding task worktrees for a workspace.
func taskBaseDir(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".sprout", taskDirName)
}

// taskWorktreePath returns the worktree path for a branch's slug.
func taskWorktreePath(workspaceRoot, slug string) string {
	return filepath.Join(taskBaseDir(workspaceRoot), slug)
}

// slugFromBranch strips the Conventional Commit type prefix from a branch
// name: "feat/add-login" → "add-login".
func slugFromBranch(branch string) string {
	if i := strings.IndexByte(branch, '/'); i >= 0 && i+1 < len(branch) {
		return branch[i+1:]
	}
	return branch
}

// createTaskWorktree creates a worktree at <workspace>/.sprout/tasks/<slug>
// on a new branch, based at base (empty means the repo's current HEAD
// branch). Unlike createIsolatedWorkspace it does not seed uncommitted
// changes: a task starts from a committed base so its branch is mergeable on
// its own.
func createTaskWorktree(ctx context.Context, workspaceRoot, branch, base string) (*taskWorktree, error) {
	repoRoot := reviewRepoRoot(ctx, workspaceRoot)
	if repoRoot == "" {
		return nil, agenterrors.NewValidation(fmt.Sprintf("%s is not inside a git repository", workspaceRoot), nil)
	}
	if _, err := runGitIn(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return nil, agenterrors.NewValidation(fmt.Sprintf("%s has no commits yet", repoRoot), nil)
	}

	if strings.TrimSpace(base) == "" {
		// A detached HEAD has no branch to merge back into, and "the current
		// branch" would be meaningless.
		current, err := runGitIn(ctx, repoRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
		if err != nil || strings.TrimSpace(current) == "" {
			return nil, agenterrors.NewValidation("the workspace is on a detached HEAD; check out a branch before starting a task", nil)
		}
		base = strings.TrimSpace(current)
	}

	if _, err := runGitIn(ctx, repoRoot, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
		return nil, agenterrors.NewValidation(fmt.Sprintf("base %q is not a commit in %s", base, repoRoot), nil)
	}

	slug := slugFromBranch(branch)
	path := taskWorktreePath(workspaceRoot, slug)
	if _, err := os.Stat(path); err == nil {
		return nil, agenterrors.NewValidation(fmt.Sprintf("a task worktree already exists at %s", path), nil)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create task directory: %w", err)
	}
	// A task worktree inside .sprout/ must not read as an untracked file in
	// the workspace: `git status` would then be permanently dirty and
	// /task merge (which refuses a dirty tree) could never run. The
	// workspace's shipped .sprout/.gitignore covers this; a workspace that
	// predates it gets the line appended here.
	if err := ensureTaskGitignore(workspaceRoot); err != nil {
		return nil, err
	}

	if _, err := runGitIn(ctx, repoRoot, "worktree", "add", "-b", branch, path, base); err != nil {
		return nil, fmt.Errorf("create task worktree: %w", err)
	}
	return &taskWorktree{
		RepoRoot:  repoRoot,
		Path:      path,
		Branch:    branch,
		Base:      base,
		Workspace: workspaceRoot,
	}, nil
}

// ensureTaskGitignore makes sure the workspace's .sprout/.gitignore covers
// the tasks directory. It never rewrites an existing file: the line is
// appended when missing, and the file itself is self-ignored so .sprout/
// does not read as untracked when nothing else in it is ignored yet.
func ensureTaskGitignore(workspaceRoot string) error {
	dir := filepath.Join(workspaceRoot, ".sprout")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create .sprout directory: %w", err)
	}
	path := filepath.Join(dir, ".gitignore")
	existing, err := os.ReadFile(path) //nolint:gosec // G304: fixed filename under the workspace root
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read .sprout/.gitignore: %w", err)
	}

	var b strings.Builder
	b.WriteString(string(existing))
	if !strings.Contains(string(existing), taskDirName+"/") {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(taskDirName + "/\n")
	}
	if !strings.Contains(string(existing), ".gitignore\n") {
		b.WriteString(".gitignore\n")
	}
	if b.String() == string(existing) {
		return nil
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write .sprout/.gitignore: %w", err)
	}
	return nil
}

// remove deletes the worktree but keeps the branch, so committed work stays
// reachable for review and merge.
func (w *taskWorktree) remove(ctx context.Context) {
	if w == nil {
		return
	}
	if _, err := runGitIn(ctx, w.RepoRoot, "worktree", "remove", "--force", w.Path); err != nil {
		_ = os.RemoveAll(w.Path)
		_, _ = runGitIn(ctx, w.RepoRoot, "worktree", "prune")
	}
}

// readWorkspaceConventions returns the workspace's AGENTS.md, capped, or ""
// when the workspace has none. The naming call uses it to follow the
// project's branch conventions instead of guessing.
func readWorkspaceConventions(workspaceRoot string) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(workspaceRoot, "AGENTS.md")) //nolint:gosec // G304: fixed filename under the workspace root
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(data))
	if len(text) > taskConventionsBudget {
		text = text[:taskConventionsBudget]
	}
	return text
}

// suggestTaskBranch makes one LLM call to name the task's branch, then
// validates the answer. An unusable answer falls back to a deterministic
// slug: naming must never block the task.
func suggestTaskBranch(ctx context.Context, a *Agent, description string) (string, error) {
	fallback := "feat/" + fallbackTaskSlug(description)
	if a == nil {
		return fallback, nil
	}

	var b strings.Builder
	b.WriteString("You name git branches. Reply with exactly one line: <type>/<slug>\n\n")
	b.WriteString("type is one of: feat, fix, refactor, chore, docs, test, perf, build, ci.\n")
	b.WriteString("slug is lowercase kebab-case, at most 6 words, describing the task.\n")
	b.WriteString("Reply with the branch name only — no quotes, no explanation.\n")
	if conventions := readWorkspaceConventions(a.currentWorkspaceRoot()); conventions != "" {
		b.WriteString("\nThe project's conventions (follow them where they mention branches or commits):\n\n")
		b.WriteString(conventions)
		b.WriteString("\n")
	}
	b.WriteString("\nTask:\n")
	b.WriteString(strings.TrimSpace(description))
	b.WriteString("\n")

	reply, err := a.GenerateResponse([]api.Message{
		{Role: "system", Content: "You name git branches. Reply with one branch name and nothing else."},
		{Role: "user", Content: b.String()},
	})
	if err != nil {
		// A provider hiccup should not cost the user their task.
		return fallback, nil
	}
	if branch, ok := parseTaskBranchName(reply); ok {
		return branch, nil
	}
	return fallback, nil
}

// parseTaskBranchName extracts and validates a branch name from a model
// reply. It accepts a bare name, a name wrapped in quotes or backticks, or a
// name on its own line inside a longer reply.
func parseTaskBranchName(reply string) (string, bool) {
	for _, line := range strings.Split(reply, "\n") {
		candidate := strings.TrimSpace(line)
		candidate = strings.Trim(candidate, "`\"'*_ ")
		candidate = strings.TrimSuffix(candidate, ".")
		if candidate == "" {
			continue
		}
		if branch, ok := normalizeTaskBranch(candidate); ok {
			return branch, true
		}
	}
	return "", false
}

// normalizeTaskBranch validates one candidate against the task branch shape
// and the length cap, returning the normalized form.
func normalizeTaskBranch(candidate string) (string, bool) {
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if !taskBranchPattern.MatchString(candidate) {
		return "", false
	}
	if len(candidate) > taskBranchMaxLen {
		// Truncate at the last complete word that still fits.
		cut := candidate[:taskBranchMaxLen]
		if i := strings.LastIndexByte(cut, '-'); i > strings.IndexByte(cut, '/')+1 {
			cut = cut[:i]
		}
		if !taskBranchPattern.MatchString(cut) {
			return "", false
		}
		candidate = cut
	}
	return candidate, true
}

// fallbackTaskSlug derives a branch slug from the task description without
// an LLM: lowercase, non-alnum collapsed to dashes, capped to a handful of
// words.
func fallbackTaskSlug(description string) string {
	slug := taskSlugSanitizer.ReplaceAllString(strings.ToLower(description), "-")
	slug = strings.Trim(slug, "-")
	words := strings.Split(slug, "-")
	if len(words) > 6 {
		words = words[:6]
	}
	slug = strings.Join(words, "-")
	if slug == "" {
		slug = "task"
	}
	if len(slug) > taskBranchMaxLen-len("feat/") {
		slug = strings.Trim(slug[:taskBranchMaxLen-len("feat/")], "-")
	}
	if slug == "" {
		slug = "task"
	}
	return slug
}
