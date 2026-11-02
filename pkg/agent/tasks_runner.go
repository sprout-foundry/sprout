package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// maxRetainedTasks bounds finished task records, mirroring the background
// registry's retention cap. A task record holds a full agent, so an
// unbounded list would pin every task's conversation state for the life of
// the process.
const maxRetainedTasks = 25

// taskRecord is the state of one background task. It lives alongside the
// background-task registry entry (same ID) so /tasks and the wakeup
// notification path work for tasks exactly as they do for subagents.
//
// Every field is written by the runner goroutine and read by /tasks on
// another goroutine, so all access goes through taskRegistry, which holds
// the lock. The struct is never read directly.
type taskRecord struct {
	ID        string
	Branch    string
	Path      string // worktree top level
	Base      string
	Prompt    string
	Started   time.Time
	Finished  time.Time
	Status    string // "running" | "completed" | "failed" | "cancelled" | "stopped" | "merged"
	Commit    string // short SHA of the task's commit, when it made one
	Summary   string // the agent's final output, for /tasks <id>
	Workspace string

	worktree *taskWorktree
}

// elapsed is called with the registry lock held.
func (t *taskRecord) elapsed() time.Duration {
	if !t.Finished.IsZero() {
		return t.Finished.Sub(t.Started)
	}
	return time.Since(t.Started)
}

// taskRegistry holds this process's tasks, keyed by task ID. The lock
// guards both the map and every mutable field of the records it holds.
type taskRegistry struct {
	mu    sync.Mutex
	order []string
	byID  map[string]*taskRecord
}

var processTasks = &taskRegistry{byID: map[string]*taskRecord{}}

// add records a task and prunes finished records past the retention cap.
func (r *taskRegistry) add(t *taskRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, t.ID)
	r.byID[t.ID] = t
	r.pruneLocked()
}

// pruneLocked drops the oldest finished records beyond maxRetainedTasks. A
// running task is never pruned.
func (r *taskRegistry) pruneLocked() {
	for len(r.order) > maxRetainedTasks {
		pruned := false
		for i, id := range r.order {
			if rec := r.byID[id]; rec == nil || rec.Status != "running" {
				delete(r.byID, id)
				r.order = append(r.order[:i], r.order[i+1:]...)
				pruned = true
				break
			}
		}
		if !pruned {
			return
		}
	}
}

// setTerminal records a task's final state.
func (r *taskRegistry) setTerminal(id, status, output, commit string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.byID[id]
	if rec == nil {
		return
	}
	rec.Status = status
	rec.Summary = output
	rec.Commit = commit
	rec.Finished = time.Now()
}

// markStopped closes a task record that a stop request ended. It is a no-op
// for IDs that are not /task records (subagent and review runs) and for
// records the runner goroutine has already finished.
func (r *taskRegistry) markStopped(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.byID[id]
	if rec == nil || rec.Status != "running" {
		return
	}
	rec.Status = "stopped"
	rec.Finished = time.Now()
}

// reserve atomically claims a concurrency slot for workspace, returning an
// error when the limit is already reached. Reserving before the LLM naming
// call and the worktree creation is what makes the limit hold under
// concurrent /task invocations.
func (r *taskRegistry) reserve(workspace string, limit int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, id := range r.order {
		if rec := r.byID[id]; rec != nil && rec.Status == "running" && rec.Workspace == workspace {
			n++
		}
	}
	if n >= limit {
		return agenterrors.NewValidation(
			fmt.Sprintf("%d tasks are already running for this workspace (limit %d); stop one with /tasks stop <id>", n, limit), nil)
	}
	return nil
}

func (r *taskRegistry) get(id string) *taskRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id]
}

// snapshot returns a copy of every record, newest first, safe to read
// without the lock.
func (r *taskRegistry) snapshot() []TaskInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TaskInfo, 0, len(r.order))
	for i := len(r.order) - 1; i >= 0; i-- {
		rec := r.byID[r.order[i]]
		if rec == nil {
			continue
		}
		out = append(out, TaskInfo{
			ID:       rec.ID,
			Branch:   rec.Branch,
			Path:     rec.Path,
			Status:   rec.Status,
			Elapsed:  rec.elapsed(),
			Commit:   rec.Commit,
			Summary:  rec.Summary,
			Worktree: rec.Path,
		})
	}
	return out
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
func (a *Agent) StartTask(ctx context.Context, opts StartTaskOptions) (*TaskInfo, error) {
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
	if err := processTasks.reserve(workspace, maxConcurrent); err != nil {
		return nil, err
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
		status, output, commit := a.runTask(bgCtx, rec, timeout)
		processTasks.setTerminal(rec.ID, status, output, commit)
		summary := taskCompletionSummary(rec.ID, rec.Branch, rec.Path, commit, status,
			time.Since(rec.Started), output)
		a.finishBackground(t, status, summary)
	}()

	info, _ := a.TaskDetail(rec.ID)
	return &info, nil
}

// runTask runs the task agent in the worktree and commits its work. It
// returns the terminal status, the agent's output, and the commit SHA.
func (a *Agent) runTask(ctx context.Context, rec *taskRecord, timeout time.Duration) (status, output, commit string) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	taskAgent, err := newTaskAgent(a, rec.Path, rec.ID)
	if err != nil {
		return "failed", fmt.Sprintf("could not start the task agent: %v", err), ""
	}

	out, runErr := taskAgent.ProcessQuery(rec.Prompt)
	switch {
	case runCtx.Err() != nil:
		return "cancelled", out, ""
	case runErr != nil:
		return "failed", fmt.Sprintf("%s\n\n%v", strings.TrimSpace(out), runErr), ""
	}

	// Commit whatever the task changed. A task that changed nothing is not a
	// failure: it may have answered a question.
	sha, commitErr := commitTaskWork(runCtx, rec.worktree)
	if commitErr != nil {
		return "failed", fmt.Sprintf("%s\n\ncould not commit the task's work: %v", strings.TrimSpace(out), commitErr), ""
	}
	return "completed", out, sha
}

// commitTaskWork commits everything in the task worktree and returns the
// short SHA. Returns "" when the worktree has no changes.
//
// Hooks are bypassed: the task worktree is unattended, and a repo hook that
// prompts or assumes a human would hang it until the task timeout.
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
	return processTasks.snapshot()
}

// TaskDetail reports one task's state and output.
func (a *Agent) TaskDetail(id string) (TaskInfo, error) {
	id = strings.TrimSpace(id)
	for _, info := range processTasks.snapshot() {
		if info.ID == id {
			return info, nil
		}
	}
	return TaskInfo{}, agenterrors.NewNotFound("task " + id)
}

// MergeTask merges a finished task's branch into the workspace's current
// branch with --no-ff. A dirty workspace or a conflicting branch leaves the
// workspace untouched.
func (a *Agent) MergeTask(ctx context.Context, id string) (string, error) {
	rec := processTasks.get(strings.TrimSpace(id))
	if rec == nil {
		return "", agenterrors.NewNotFound("task " + id)
	}
	processTasks.mu.Lock()
	status, branch, path, wt := rec.Status, rec.Branch, rec.Path, rec.worktree
	processTasks.mu.Unlock()

	if status == "running" {
		return "", agenterrors.NewValidation(fmt.Sprintf("task %s is still running; stop it with /tasks stop %s first", id, id), nil)
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
	if _, err := runGitIn(ctx, repoRoot, "merge", "--no-commit", "--no-ff", branch); err != nil {
		_, _ = runGitIn(ctx, repoRoot, "merge", "--abort")
		return "", fmt.Errorf("merge %s into the workspace: %w", branch, err)
	}
	// The merge commit lands on the user's own branch, so the repo's hooks
	// run: this is the one commit in the feature a human would have made.
	if _, err := runGitIn(ctx, repoRoot, "commit", "-q", "-m", fmt.Sprintf("merge task %s: %s", id, branch)); err != nil {
		return "", fmt.Errorf("commit the merge: %w", err)
	}

	wt.remove(ctx)
	processTasks.setTerminal(id, "merged", "", "")
	return fmt.Sprintf("Merged %s into the workspace. The worktree at %s was removed; branch %s is kept.",
		branch, path, branch), nil
}
