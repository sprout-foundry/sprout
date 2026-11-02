package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// resetProcessTasks clears the process-wide task registry between tests. The
// registry is process state (like the background-task registry), so tests
// that count running tasks must start from empty.
func resetProcessTasks(t *testing.T) {
	t.Helper()
	processTasks.mu.Lock()
	processTasks.order = nil
	processTasks.byID = map[string]*taskRecord{}
	processTasks.mu.Unlock()
	t.Cleanup(func() {
		processTasks.mu.Lock()
		processTasks.order = nil
		processTasks.byID = map[string]*taskRecord{}
		processTasks.mu.Unlock()
	})
}

// newTaskTestAgent returns a primary agent rooted at repo, with a scripted
// client so task runs need no provider.
func newTaskTestAgent(t *testing.T, repo string) *Agent {
	t.Helper()
	a := newTestAgent(t)
	a.SetWorkspaceRoot(repo)
	a.GetSubagentRunner().testClientFactory = func(_ api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(NewScriptedResponseBuilder().Content("task done").Build()), nil
	}
	return a
}

func TestTaskCompletionSummary_NamesBranchAndMergeCommand(t *testing.T) {
	rec := &taskRecord{
		ID:      "bg-task-1",
		Branch:  "feat/add-login",
		Path:    "/tmp/ws/.sprout/tasks/add-login",
		Commit:  "abc1234",
		Started: time.Now().Add(-2 * time.Minute),
	}
	got := taskCompletionSummary(rec, "completed", "did the thing")

	for _, want := range []string{"feat/add-login", "abc1234", "/task merge bg-task-1", "git merge --no-ff feat/add-login", "did the thing"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestTaskCompletionSummary_FailedRunSaysUncommitted(t *testing.T) {
	rec := &taskRecord{ID: "bg-task-2", Branch: "fix/x", Path: "/tmp/wt", Started: time.Now()}
	got := taskCompletionSummary(rec, "failed", "")
	if !strings.Contains(got, "uncommitted") {
		t.Errorf("failed summary should say the worktree is uncommitted:\n%s", got)
	}
	if strings.Contains(got, "/task merge") {
		t.Errorf("failed summary should not offer a merge:\n%s", got)
	}
}

func TestTruncateTaskOutput_KeepsTail(t *testing.T) {
	out := strings.Repeat("a", 100) + "CONCLUSION"
	got := truncateTaskOutput(out, 50)
	if !strings.HasSuffix(got, "CONCLUSION") {
		t.Errorf("truncated output lost the tail: %q", got)
	}
	if len(got) > 50+len("…(truncated)…\n") {
		t.Errorf("truncated output is %d bytes, over the limit", len(got))
	}
	if short := truncateTaskOutput("short", 50); short != "short" {
		t.Errorf("short output was modified: %q", short)
	}
}

func TestCommitTaskWork_CommitsChanges(t *testing.T) {
	repo := initTaskTestRepo(t)
	wt, err := createTaskWorktree(context.Background(), repo, "feat/commit-me", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { wt.remove(context.Background()) })

	if sha, err := commitTaskWork(context.Background(), wt); err != nil || sha != "" {
		t.Fatalf("empty worktree: sha = %q, err = %v; want no commit", sha, err)
	}

	if err := os.WriteFile(filepath.Join(wt.Path, "new.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := commitTaskWork(context.Background(), wt)
	if err != nil {
		t.Fatalf("commitTaskWork: %v", err)
	}
	if sha == "" {
		t.Fatal("expected a commit SHA")
	}

	subject, err := runGitIn(context.Background(), wt.Path, "log", "-1", "--pretty=%s")
	if err != nil {
		t.Fatalf("read commit: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(subject), "task: ") {
		t.Errorf("commit subject = %q, want a task: prefix", strings.TrimSpace(subject))
	}
}

func TestMergeTask_RefusesRunningTask(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	a := newTaskTestAgent(t, repo)

	processTasks.add(&taskRecord{ID: "bg-task-run", Branch: "feat/running", Path: repo, Status: "running", Workspace: repo})

	if _, err := a.MergeTask(context.Background(), "bg-task-run"); err == nil {
		t.Fatal("expected an error merging a running task")
	}
}

func TestMergeTask_RefusesDirtyWorkspace(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	a := newTaskTestAgent(t, repo)

	wt, err := createTaskWorktree(context.Background(), repo, "feat/merge-me", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { wt.remove(context.Background()) })
	processTasks.add(&taskRecord{ID: "bg-task-dirty", Branch: wt.Branch, Path: wt.Path, Status: "completed", Workspace: repo, worktree: wt})

	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MergeTask(context.Background(), "bg-task-dirty"); err == nil {
		t.Fatal("expected an error merging into a dirty workspace")
	}
}

func TestMergeTask_MergesBranchAndRemovesWorktree(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	a := newTaskTestAgent(t, repo)

	wt, err := createTaskWorktree(context.Background(), repo, "feat/merge-ok", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "feature.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := commitTaskWork(context.Background(), wt); err != nil {
		t.Fatalf("commitTaskWork: %v", err)
	}
	processTasks.add(&taskRecord{ID: "bg-task-ok", Branch: wt.Branch, Path: wt.Path, Status: "completed", Workspace: repo, worktree: wt})

	msg, err := a.MergeTask(context.Background(), "bg-task-ok")
	if err != nil {
		t.Fatalf("MergeTask: %v", err)
	}
	if !strings.Contains(msg, "feat/merge-ok") {
		t.Errorf("merge message = %q, want the branch name", msg)
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Errorf("merged file missing from the workspace: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Errorf("worktree still exists after merge: %v", err)
	}
	if _, err := runGitIn(context.Background(), repo, "rev-parse", "--verify", "--quiet", "feat/merge-ok"); err != nil {
		t.Errorf("branch was deleted by the merge: %v", err)
	}
}

func TestMergeTask_ConflictLeavesWorkspaceUntouched(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	a := newTaskTestAgent(t, repo)

	shared := filepath.Join(repo, "shared.txt")
	if err := os.WriteFile(shared, []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGitIn(context.Background(), repo, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGitIn(context.Background(), repo, "commit", "-q", "-m", "base"); err != nil {
		t.Fatal(err)
	}

	wt, err := createTaskWorktree(context.Background(), repo, "feat/conflict", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { wt.remove(context.Background()) })
	if err := os.WriteFile(filepath.Join(wt.Path, "shared.txt"), []byte("task side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := commitTaskWork(context.Background(), wt); err != nil {
		t.Fatalf("commitTaskWork: %v", err)
	}

	// The primary moves the same line meanwhile.
	if err := os.WriteFile(shared, []byte("primary side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGitIn(context.Background(), repo, "commit", "-qam", "primary"); err != nil {
		t.Fatal(err)
	}
	processTasks.add(&taskRecord{ID: "bg-task-conflict", Branch: wt.Branch, Path: wt.Path, Status: "completed", Workspace: repo, worktree: wt})

	if _, err := a.MergeTask(context.Background(), "bg-task-conflict"); err == nil {
		t.Fatal("expected a conflict error")
	}

	data, err := os.ReadFile(shared)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "primary side\n" {
		t.Errorf("workspace file = %q, want the primary's content untouched", string(data))
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Errorf("a merge is still in progress after a conflict: %v", err)
	}
}

func TestStartTask_RefusesEmptyDescription(t *testing.T) {
	resetProcessTasks(t)
	a := newTaskTestAgent(t, initTaskTestRepo(t))
	if _, err := a.StartTask(context.Background(), StartTaskOptions{Description: "   "}); err == nil {
		t.Fatal("expected an error for an empty description")
	}
}

func TestStartTask_RefusesBeyondConcurrencyLimit(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	a := newTaskTestAgent(t, repo)

	for i := 0; i < 2; i++ {
		processTasks.add(&taskRecord{ID: "bg-task-busy-" + string(rune('a'+i)), Status: "running", Workspace: repo})
	}
	_, err := a.StartTask(context.Background(), StartTaskOptions{Description: "do work", MaxConcurrent: 2})
	if err == nil {
		t.Fatal("expected an error past the concurrency limit")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %v, want it to name the running tasks", err)
	}
}

func TestTaskDetail_NotFound(t *testing.T) {
	resetProcessTasks(t)
	a := newTaskTestAgent(t, initTaskTestRepo(t))
	if _, err := a.TaskDetail("bg-task-missing"); err == nil {
		t.Fatal("expected a not-found error")
	}
}
