package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// isolationGitBudget bounds git commands that touch a whole worktree (add,
// apply, remove), which can take longer than the review helpers' budget on
// large repositories.
const isolationGitBudget = 2 * time.Minute

// isolatedWorkspace is a git worktree seeded with the primary workspace's
// uncommitted state, where a file-modifying subagent can run without its
// edits colliding with the primary's. Its changes come back as a patch
// against the seeded baseline.
type isolatedWorkspace struct {
	repoRoot string // primary repository top level, as git reports it (symlinks resolved)
	// displayRoot is repoRoot in the form the primary workspace path uses
	// (e.g. macOS /var/... rather than /private/var/...), so remapped change
	// paths match the primary tracker's own entries for the same files.
	displayRoot string
	path        string // worktree top level
	subdir      string // workspace root relative to repoRoot ("" at top level)
	baseline    string // commit in the worktree holding the seeded state
	patchPath   string // where an unapplied patch is kept
}

func runGitIn(ctx context.Context, dir string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, isolationGitBudget)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", append([]string{"-C", dir, "--no-pager", "-c", "color.ui=false"}, args...)...) //nolint:gosec // G204: fixed git subcommands on sprout-managed worktrees
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=sprout", "GIT_AUTHOR_EMAIL=sprout@localhost",
		"GIT_COMMITTER_NAME=sprout", "GIT_COMMITTER_EMAIL=sprout@localhost")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// isolatedWorktreesDir is where isolated worktrees and kept patches live.
// State, not cache: a worktree kept after a conflict holds work to merge.
func isolatedWorktreesDir() (string, error) {
	state, err := envutil.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "worktrees"), nil
}

// createIsolatedWorkspace creates a worktree of the repository containing
// workspaceRoot, at HEAD plus the current uncommitted changes and untracked
// files, and commits that state as the baseline.
func createIsolatedWorkspace(ctx context.Context, workspaceRoot, id string) (*isolatedWorkspace, error) {
	repoRoot := reviewRepoRoot(ctx, workspaceRoot)
	if repoRoot == "" {
		return nil, fmt.Errorf("isolation needs a git repository; %s is not in one", workspaceRoot)
	}
	if _, err := runGitIn(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return nil, fmt.Errorf("isolation needs at least one commit in %s", repoRoot)
	}
	base, err := isolatedWorktreesDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%s-%s-%d", filepath.Base(repoRoot), id, time.Now().Unix())
	ws := &isolatedWorkspace{
		repoRoot:  repoRoot,
		path:      filepath.Join(base, name),
		patchPath: filepath.Join(base, name+".patch"),
	}
	// Compare in resolved form (git resolves symlinks), but keep the
	// workspace's own form for paths recorded in change history.
	ws.displayRoot = repoRoot
	realWorkspace := workspaceRoot
	if r, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		realWorkspace = r
	}
	if rel, err := filepath.Rel(repoRoot, realWorkspace); err == nil && !strings.HasPrefix(rel, "..") {
		if rel != "." {
			ws.subdir = rel
		}
		ws.displayRoot = filepath.Clean(strings.TrimSuffix(filepath.Clean(workspaceRoot), string(filepath.Separator)+rel))
		if rel == "." {
			ws.displayRoot = filepath.Clean(workspaceRoot)
		}
	}

	if _, err := runGitIn(ctx, repoRoot, "worktree", "add", "--detach", ws.path, "HEAD"); err != nil {
		return nil, err
	}
	if err := ws.seed(ctx); err != nil {
		ws.remove(context.Background())
		return nil, err
	}
	return ws, nil
}

// seed copies the primary's uncommitted state into the worktree and commits
// it as the baseline the subagent's changes are measured against.
func (w *isolatedWorkspace) seed(ctx context.Context) error {
	diff, err := runGitIn(ctx, w.repoRoot, "diff", "--binary", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(diff) != "" {
		if err := w.applyPatch(ctx, w.path, diff, false); err != nil {
			return fmt.Errorf("seed uncommitted changes: %w", err)
		}
	}
	for _, rel := range listUntracked(ctx, w.repoRoot, nil) {
		if err := copyFile(filepath.Join(w.repoRoot, rel), filepath.Join(w.path, rel)); err != nil {
			return fmt.Errorf("seed untracked %s: %w", rel, err)
		}
	}
	if _, err := runGitIn(ctx, w.path, "add", "-A"); err != nil {
		return err
	}
	if _, err := runGitIn(ctx, w.path, "-c", "core.hooksPath=/dev/null", "commit", "--no-verify", "--allow-empty", "-q", "-m", "sprout isolation baseline"); err != nil {
		return err
	}
	head, err := runGitIn(ctx, w.path, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	w.baseline = strings.TrimSpace(head)
	return nil
}

// dir is the directory the subagent works in: the worktree counterpart of
// the primary's workspace root.
func (w *isolatedWorkspace) dir() string {
	return filepath.Join(w.path, w.subdir)
}

// collectPatch returns the subagent's changes relative to the baseline,
// including new and deleted files.
func (w *isolatedWorkspace) collectPatch(ctx context.Context) (string, error) {
	if _, err := runGitIn(ctx, w.path, "add", "-A"); err != nil {
		return "", err
	}
	return runGitIn(ctx, w.path, "diff", "--cached", "--binary", w.baseline)
}

// applyPatch applies patch in dir, checking first so a patch that doesn't
// apply cleanly leaves dir untouched.
func (w *isolatedWorkspace) applyPatch(ctx context.Context, dir, patch string, keepOnFailure bool) error {
	f, err := os.CreateTemp("", "sprout-isolation-*.patch")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(patch); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, err := runGitIn(ctx, dir, "apply", "--check", "--whitespace=nowarn", f.Name()); err != nil {
		if keepOnFailure {
			_ = os.WriteFile(w.patchPath, []byte(patch), 0o644)
		}
		return err
	}
	_, err = runGitIn(ctx, dir, "apply", "--whitespace=nowarn", f.Name())
	return err
}

func (w *isolatedWorkspace) remove(ctx context.Context) {
	if _, err := runGitIn(ctx, w.repoRoot, "worktree", "remove", "--force", w.path); err != nil {
		_ = os.RemoveAll(w.path)
		_, _ = runGitIn(ctx, w.repoRoot, "worktree", "prune")
	}
}

// remapChanges rewrites tracked change paths from the worktree to the
// primary repository, so the primary's change history names its own files.
func (w *isolatedWorkspace) remapChanges(changes []TrackedFileChange) []TrackedFileChange {
	roots := []string{w.path}
	if real, err := filepath.EvalSymlinks(w.path); err == nil && real != w.path {
		roots = append(roots, real)
	}
	remap := func(p string) string {
		if !filepath.IsAbs(p) {
			return p
		}
		for _, root := range roots {
			if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.Join(w.displayRoot, rel)
			}
		}
		return p
	}
	out := make([]TrackedFileChange, len(changes))
	for i, c := range changes {
		c.FilePath = remap(c.FilePath)
		if len(c.BulkItems) > 0 {
			items := make([]TrackedBulkItem, len(c.BulkItems))
			for j, item := range c.BulkItems {
				item.FilePath = remap(item.FilePath)
				items[j] = item
			}
			c.BulkItems = items
		}
		out[i] = c
	}
	return out
}

func copyFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	data, err := os.ReadFile(src) //nolint:gosec // G304: untracked file listed by git in the primary repository
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm()) //nolint:gosec // G703: dst is a git-listed repo-relative path joined to the sprout-created worktree
}

// isolationPreamble tells an isolated subagent where it is working, so paths
// to the primary workspace in its task aren't edited directly.
func isolationPreamble(w *isolatedWorkspace, originalRoot string) string {
	return fmt.Sprintf("# Isolated Workspace\n\nYou are working in an isolated copy of the repository at `%s` (it already includes the current uncommitted changes). "+
		"Paths under `%s` in this task refer to the same files in your copy — read and edit them under `%s`, never under `%s`. "+
		"Your changes are applied back to the original workspace when you finish.\n\n---\n\n",
		w.dir(), originalRoot, w.dir(), originalRoot)
}

// runIsolatedSubagent runs spec in an isolated worktree and brings its
// changes back. On a clean finish the patch is applied to the primary
// workspace; if it no longer applies (the primary changed the same lines
// meanwhile), or the run failed or was cancelled, the primary is left
// untouched and the outcome is reported in the result.
func runIsolatedSubagent(ctx context.Context, a *Agent, spec *subagentLaunchSpec, taskID string, quiet bool) *SubagentResult {
	originalRoot := spec.subagentWorkspaceRoot
	if originalRoot == "" {
		originalRoot = a.currentWorkspaceRoot()
	}
	ws, err := createIsolatedWorkspace(ctx, originalRoot, taskID)
	if err != nil {
		return &SubagentResult{ID: taskID, Error: fmt.Errorf("create isolated workspace: %w", err)}
	}

	result := a.GetSubagentRunner().runTask(ctx, taskID, isolationPreamble(ws, originalRoot)+spec.enhancedPrompt, SubagentOptions{
		Persona:      spec.persona,
		Model:        spec.model,
		Provider:     spec.provider,
		SystemPrompt: spec.systemPromptText,
		WorkingDir:   ws.dir(),
		Quiet:        quiet,
	}, nil, 0)
	if result == nil {
		result = &SubagentResult{ID: taskID, Error: fmt.Errorf("subagent produced no result")}
	}

	// Bringing changes back must survive the run's own cancellation.
	note := finishIsolatedRun(context.Background(), ws, result)
	result.Output = "[isolated run] " + note + "\n\n" + result.Output
	return result
}

// finishIsolatedRun brings an isolated run's changes back and returns a note
// describing the outcome. Changes are applied only when the run finished
// cleanly and the patch still applies; otherwise the primary workspace is
// left untouched and the work is kept for manual review.
func finishIsolatedRun(ctx context.Context, ws *isolatedWorkspace, result *SubagentResult) string {
	patch, patchErr := ws.collectPatch(ctx)
	switch {
	case result.Cancelled:
		ws.remove(ctx)
		result.FileChanges = nil
		return "The run was cancelled; its changes were discarded."
	case patchErr != nil:
		result.FileChanges = nil
		return fmt.Sprintf("Could not collect the changes (%v). The work is kept in %s.", patchErr, ws.path)
	case strings.TrimSpace(patch) == "":
		ws.remove(ctx)
		return "No file changes."
	case result.Error != nil || result.BudgetExceeded || result.Truncated:
		_ = os.WriteFile(ws.patchPath, []byte(patch), 0o644)
		result.FileChanges = nil
		return fmt.Sprintf("The run did not finish cleanly, so its partial changes were NOT applied. They are kept in %s (patch: %s).", ws.path, ws.patchPath)
	}
	if err := ws.applyPatch(ctx, ws.repoRoot, patch, true); err != nil {
		result.FileChanges = nil
		return fmt.Sprintf("The changes no longer apply cleanly to the workspace (it changed meanwhile): %v. "+
			"Nothing was changed. The work is kept in %s; to merge it, run `git apply --3way %s` in %s and resolve any conflicts.",
			err, ws.path, ws.patchPath, ws.repoRoot)
	}
	result.FileChanges = ws.remapChanges(result.FileChanges)
	ws.remove(ctx)
	return fmt.Sprintf("Changes applied to the workspace (%d tracked file change(s)).", len(result.FileChanges))
}
