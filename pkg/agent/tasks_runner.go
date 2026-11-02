package agent

import (
	"context"
	"fmt"

	"strings"
	"sync"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// taskRecord is the state of one background task. It lives alongside the
// background-task registry entry (same ID) so /tasks and the wakeup
// notification path work for tasks exactly as they do for subagents.
type taskRecord struct {
	ID        string
	Branch    string
	Path      string // worktree top level
	Base      string
	Prompt    string
	Started   time.Time
	Finished  time.Time
	Status    string // "running" | "completed" | "failed" | "cancelled" | "stopped"
	Commit    string // short SHA of the task's commit, when it made one
	Summary   string // the agent's final output, for /tasks <id>
	Workspace string

	worktree *taskWorktree
	agent    *Agent
}

func (t *taskRecord) elapsed() time.Duration {
	if !t.Finished.IsZero() {
		return t.Finished.Sub(t.Started)
	}
	return time.Since(t.Started)
}

// taskRegistry holds this process's tasks, keyed by task ID.
type taskRegistry struct {
	mu    sync.Mutex
	order []string
	byID  map[string]*taskRecord
}

var (
	processTasks   = &taskRegistry{byID: map[string]*taskRecord{}}
	processTasksMu sync.Mutex
)

func (r *taskRegistry) add(t *taskRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, t.ID)
	r.byID[t.ID] = t
}

func (r *taskRegistry) get(id string) *taskRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id]
}

func (r *taskRegistry) all() []*taskRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*taskRecord, 0, len(r.order))
	for _, id := range r.order {
		if t := r.byID[id]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

// running counts unfinished tasks for this workspace.
func (r *taskRegistry) running(workspace string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, id := range r.order {
		if t := r.byID[id]; t != nil && t.Status == "running" && t.Workspace == workspace {
			n++
		}
	}
	return n
}

// StartTaskOptions configures a new background task.
type StartTaskOptions struct {
	// Description is the task text handed to the task agent.
	Description string
	// Base is the branch or commit to create the worktree from. Empty means
	// the workspace's current branch.
	Base string
	// MaxConcurrent bounds how many tasks may run at once for this workspace.
	// Zero uses the default.
	MaxConcurrent int
	// Timeout bounds the task's wall clock. Zero uses the default.
	Timeout time.Duration
}

const (
	defaultTaskMaxConcurrent = 3
	defaultTaskTimeout       = 90 * time.Minute
)

// StartTask creates a worktree on a newly named branch and runs the task in
// the background. It returns as soon as the task is registered.
func (a *Agent) StartTask(ctx context.Context, opts StartTaskOptions) (*taskRecord, error) {
	if a == nil {
		return nil, agenterrors.NewValidation("task needs an agent", nil)
	}
	description := strings.TrimSpace(opts.Description)
	if description == "" {
		return nil, agenterrors.NewValidation("usage: /task <description>", nil)
	}
	workspace := a.currentWorkspaceRoot()

	maxConcurrent := opts.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = defaultTaskMaxConcurrent
	}
	if running := processTasks.running(workspace); running >= maxConcurrent {
		return nil, agenterrors.NewValidation(
			fmt.Sprintf("%d tasks are already running for this workspace (limit %d); stop one with /tasks stop <id>", running, maxConcurrent), nil)
	}

	branch, err := suggestTaskBranch(ctx, a, description)
	if err != nil {
		return nil, err
	}
	wt, err := createTaskWorktree(ctx, workspace, branch, opts.Base)
	if err != nil {
		return nil, err
	}

	runner := a.GetSubagentRunner()
	bgCtx, cancel := context.WithCancel(context.Background())
	t := runner.registerBackground("task", taskRole, ShortCommandLabel(description), cancel)

	rec := &taskRecord{
		ID:        t.ID,
		Branch:    wt.Branch,
		Path:      wt.Path,
		Base:      wt.Base,
		Prompt:    description,
		Started:   time.Now(),
		Status:    "running",
		Workspace: workspace,
		worktree:  wt,
	}
	processTasks.add(rec)

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}

	go func() {
		defer cancel()
		status, output := a.runTask(bgCtx, rec, timeout)
		rec.Finished = time.Now()
		rec.Status = status
		rec.Summary = output
		a.finishBackground(t, status, taskCompletionSummary(rec, status, output))
	}()

	return rec, nil
}

// runTask runs the task agent in the worktree and commits its work. It
// returns the terminal status and the agent's output.
func (a *Agent) runTask(ctx context.Context, rec *taskRecord, timeout time.Duration) (string, string) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	taskAgent, err := newTaskAgent(a, rec.Path, rec.ID)
	if err != nil {
		return "failed", fmt.Sprintf("could not start the task agent: %v", err)
	}
	rec.agent = taskAgent

	output, runErr := taskAgent.ProcessQuery(rec.Prompt)
	switch {
	case runCtx.Err() != nil:
		return "cancelled", output
	case runErr != nil:
		return "failed", fmt.Sprintf("%s\n\n%v", strings.TrimSpace(output), runErr)
	}

	// Commit whatever the task changed. A task that changed nothing is not a
	// failure: it may have answered a question.
	commit, commitErr := commitTaskWork(ctx, rec.worktree)
	if commitErr != nil {
		return "failed", fmt.Sprintf("%s\n\ncould not commit the task's work: %v", strings.TrimSpace(output), commitErr)
	}
	rec.Commit = commit
	return "completed", output
}

// commitTaskWork commits everything in the task worktree and returns the
// short SHA. Returns "" when the worktree has no changes.
func commitTaskWork(ctx context.Context, wt *taskWorktree) (string, error) {
	if wt == nil {
		return "", agenterrors.NewValidation("task has no worktree", nil)
	}
	if _, err := runGitIn(ctx, wt.Path, "add", "-A"); err != nil {
		return "", err
	}
	status, err := runGitIn(ctx, wt.Path, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) == "" {
		return "", nil
	}
	msg := fmt.Sprintf("task: %s", ShortCommandLabel(wt.Branch))
	if _, err := runGitIn(ctx, wt.Path, "-c", "core.hooksPath=/dev/null", "commit", "--no-verify", "-q", "-m", msg); err != nil {
		return "", err
	}
	sha, err := runGitIn(ctx, wt.Path, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// TaskInfo is the display form of a task, for /tasks and the WebUI.
type TaskInfo struct {
	ID       string
	Branch   string
	Path     string
	Status   string
	Elapsed  time.Duration
	Commit   string
	Summary  string
	Worktree string
}

// Tasks returns this process's tasks, newest first.
func (a *Agent) Tasks() []TaskInfo {
	recs := processTasks.all()
	out := make([]TaskInfo, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		out = append(out, TaskInfo{
			ID:       r.ID,
			Branch:   r.Branch,
			Path:     r.Path,
			Status:   r.Status,
			Elapsed:  r.elapsed(),
			Commit:   r.Commit,
			Summary:  r.Summary,
			Worktree: r.Path,
		})
	}
	return out
}

// TaskDetail reports one task's state and output.
func (a *Agent) TaskDetail(id string) (TaskInfo, error) {
	r := processTasks.get(strings.TrimSpace(id))
	if r == nil {
		return TaskInfo{}, agenterrors.NewNotFound("task " + id)
	}
	return TaskInfo{
		ID:       r.ID,
		Branch:   r.Branch,
		Path:     r.Path,
		Status:   r.Status,
		Elapsed:  r.elapsed(),
		Commit:   r.Commit,
		Summary:  r.Summary,
		Worktree: r.Path,
	}, nil
}

// MergeTask merges a finished task's branch into the workspace's current
// branch with --no-ff. A dirty workspace or a conflicting branch leaves the
// workspace untouched.
func (a *Agent) MergeTask(ctx context.Context, id string) (string, error) {
	rec := processTasks.get(strings.TrimSpace(id))
	if rec == nil {
		return "", agenterrors.NewNotFound("task " + id)
	}
	if rec.Status == "running" {
		return "", agenterrors.NewValidation(fmt.Sprintf("task %s is still running; stop it with /tasks stop %s first", rec.ID, rec.ID), nil)
	}

	workspace := a.currentWorkspaceRoot()
	repoRoot := reviewRepoRoot(ctx, workspace)
	if repoRoot == "" {
		return "", agenterrors.NewValidation(fmt.Sprintf("%s is not inside a git repository", workspace), nil)
	}

	// Refuse a dirty tree: a merge that lands on top of uncommitted work is
	// the one thing the user cannot easily undo.
	if dirty, err := runGitIn(ctx, repoRoot, "status", "--porcelain"); err != nil {
		return "", err
	} else if strings.TrimSpace(dirty) != "" {
		return "", agenterrors.NewValidation("the workspace has uncommitted changes; commit or stash them before merging a task", nil)
	}

	// --no-commit first so a conflict aborts without leaving a half-merged
	// tree behind.
	if _, err := runGitIn(ctx, repoRoot, "merge", "--no-commit", "--no-ff", rec.Branch); err != nil {
		_, _ = runGitIn(ctx, repoRoot, "merge", "--abort")
		return "", fmt.Errorf("merge %s into the workspace: %w", rec.Branch, err)
	}
	if _, err := runGitIn(ctx, repoRoot, "-c", "core.hooksPath=/dev/null", "commit", "--no-verify", "-q", "-m",
		fmt.Sprintf("merge task %s: %s", rec.ID, rec.Branch)); err != nil {
		return "", fmt.Errorf("commit the merge: %w", err)
	}

	rec.worktree.remove(ctx)
	rec.Status = "merged"
	return fmt.Sprintf("Merged %s into the workspace. The worktree at %s was removed; branch %s is kept.",
		rec.Branch, rec.Path, rec.Branch), nil
}
