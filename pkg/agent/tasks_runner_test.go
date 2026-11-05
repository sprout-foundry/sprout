package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// resetProcessTasks clears the process-wide task registry between tests. The
// registry is process state (like the background-task registry), so tests
// that count running tasks must start from empty.
func resetProcessTasks(t *testing.T) {
	t.Helper()
	processTasks.mu.Lock()
	processTasks.order = nil
	processTasks.byID = map[string]*taskRecord{}
	processTasks.pending = nil
	processTasks.mu.Unlock()
	t.Cleanup(func() {
		processTasks.mu.Lock()
		processTasks.order = nil
		processTasks.byID = map[string]*taskRecord{}
		processTasks.pending = nil
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
	got := taskCompletionSummary("bg-task-1", "feat/add-login", "/tmp/ws/.sprout/tasks/add-login",
		"abc1234", "completed", 2*time.Minute, "did the thing")

	for _, want := range []string{"feat/add-login", "abc1234", "/task merge bg-task-1", "git merge --no-ff feat/add-login", "did the thing"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestTaskCompletionSummary_FailedRunSaysUncommitted(t *testing.T) {
	got := taskCompletionSummary("bg-task-2", "fix/x", "/tmp/wt", "", "failed", time.Second, "")
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

// A task runs concurrently with the primary's turn, so it must not run the
// per-turn hooks that assume one turn per agent at a time.
func TestNewTaskAgent_IsIsolatedFromPrimaryTurnState(t *testing.T) {
	repo := initTaskTestRepo(t)
	primary := newTaskTestAgent(t, repo)
	primary.SetSessionID("primary-session")

	wt, err := createTaskWorktree(context.Background(), repo, "feat/isolated", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { wt.remove(context.Background()) })

	taskAgent, err := newTaskAgent(primary, wt.Path, "bg-task-9")
	if err != nil {
		t.Fatalf("newTaskAgent: %v", err)
	}

	if got := taskAgent.GetWorkspaceRoot(); got != wt.Path {
		t.Errorf("workspace root = %q, want the worktree %q", got, wt.Path)
	}
	if got := taskAgent.GetRole(); got != taskRole {
		t.Errorf("role = %q, want %q", got, taskRole)
	}
	if !taskAgent.IsWorkflowRun() {
		t.Error("a task must run non-interactively (SetWorkflowRun) so no approval prompt can block it")
	}
	if !taskAgent.IsSubagent() {
		t.Error("a task must be marked as a delegated run so the turn-end quality/verification hooks and progress events stay off")
	}
	if got := taskAgent.GetSessionID(); got != "task_bg-task-9" {
		t.Errorf("session id = %q, want task_bg-task-9", got)
	}
	if taskAgent.state == primary.state {
		t.Error("task shares the primary's conversation state")
	}
	if taskAgent.changeTracker == primary.changeTracker {
		t.Error("task shares the primary's change tracker")
	}
	if taskAgent.todoMgr == primary.todoMgr {
		t.Error("task shares the primary's todo manager")
	}
	if got := taskAgent.GetMaxIterations(); got != primary.GetMaxIterations() {
		t.Errorf("max iterations = %d, want the primary's %d (not a persona budget)", got, primary.GetMaxIterations())
	}
}

// The background-task ID is what /tasks stop, /task merge and the notification
// all key on, so a stopped task must reach a terminal state the rest of the
// feature can act on.
func TestTaskRegistry_SetTerminalReleasesSlot(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	processTasks.add(&taskRecord{ID: "bg-task-1", Status: "running", Workspace: repo, Started: time.Now()})

	if _, reserveErr := processTasks.reserve(repo, 1); reserveErr == nil {
		t.Fatal("a running task should occupy the only slot")
	}

	processTasks.setTerminal("bg-task-1", "stopped", "", "")

	if _, reserveErr := processTasks.reserve(repo, 1); reserveErr != nil {
		t.Errorf("a stopped task should release its slot: %v", reserveErr)
	}
	info := processTasks.snapshot()[0]
	if info.Status != "stopped" {
		t.Errorf("status = %q, want stopped", info.Status)
	}
	if got := processTasks.get("bg-task-1").elapsed(); got <= 0 {
		t.Errorf("elapsed = %v, want a positive duration once finished", got)
	}
}

func TestTaskRegistry_ReserveIsPerWorkspace(t *testing.T) {
	resetProcessTasks(t)
	processTasks.add(&taskRecord{ID: "bg-task-a", Status: "running", Workspace: "/ws/a", Started: time.Now()})

	if _, reserveErr := processTasks.reserve("/ws/b", 1); reserveErr != nil {
		t.Errorf("a task in another workspace should not consume this workspace's slot: %v", reserveErr)
	}
}

func TestTaskRegistry_PrunesFinishedBeyondCap(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	for i := 0; i < maxRetainedTasks+5; i++ {
		id := fmt.Sprintf("bg-task-%d", i)
		processTasks.add(&taskRecord{ID: id, Status: "running", Workspace: repo, Started: time.Now()})
		processTasks.setTerminal(id, "completed", "", "")
	}

	got := len(processTasks.snapshot())
	if got > maxRetainedTasks {
		t.Errorf("retained %d finished tasks, want at most %d", got, maxRetainedTasks)
	}
}

func TestTaskRegistry_NeverPrunesRunning(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	processTasks.add(&taskRecord{ID: "bg-task-live", Status: "running", Workspace: repo, Started: time.Now()})
	for i := 0; i < maxRetainedTasks+5; i++ {
		id := fmt.Sprintf("bg-task-done-%d", i)
		processTasks.add(&taskRecord{ID: id, Status: "running", Workspace: repo, Started: time.Now()})
		processTasks.setTerminal(id, "completed", "", "")
	}

	if processTasks.get("bg-task-live") == nil {
		t.Error("a running task was pruned")
	}
}

// A stop request must close the task record, or /task merge refuses forever
// and the concurrency slot is never released.
func TestTaskRegistry_MarkStopped(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	processTasks.add(&taskRecord{ID: "bg-task-stop", Status: "running", Workspace: repo, Started: time.Now()})

	processTasks.markStopped("bg-task-stop")

	rec := processTasks.get("bg-task-stop")
	if rec.Status != "stopped" {
		t.Errorf("status = %q, want stopped", rec.Status)
	}
	if rec.Finished.IsZero() {
		t.Error("Finished was not set")
	}
	if _, reserveErr := processTasks.reserve(repo, 1); reserveErr != nil {
		t.Errorf("a stopped task should release its slot: %v", reserveErr)
	}
}

func TestTaskRegistry_MarkStoppedIgnoresUnknownAndFinished(t *testing.T) {
	resetProcessTasks(t)
	processTasks.markStopped("bg-subagent-1")

	processTasks.add(&taskRecord{ID: "bg-task-done", Status: "completed", Workspace: "/ws", Started: time.Now()})
	processTasks.markStopped("bg-task-done")
	if got := processTasks.get("bg-task-done").Status; got != "completed" {
		t.Errorf("status = %q, want the completed status preserved", got)
	}
}

// Counting running records is not enough: the slot must be held from the
// moment it is reserved, or two concurrent /task invocations both pass the
// check while the LLM naming call and worktree creation are in flight.
func TestTaskRegistry_ReserveHoldsSlotBeforeAdopt(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)

	first, reserveErr := processTasks.reserve(repo, 1)
	if reserveErr != nil {
		t.Fatalf("first reserve: %v", reserveErr)
	}
	if _, secondErr := processTasks.reserve(repo, 1); secondErr == nil {
		t.Fatal("a reserved-but-unadopted slot must still count against the limit")
	}

	processTasks.discard(first)
	if _, thirdErr := processTasks.reserve(repo, 1); thirdErr != nil {
		t.Errorf("discarding a reservation should free the slot: %v", thirdErr)
	}
}

func TestTaskRegistry_AdoptRegistersUnderID(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)

	rec, reserveErr := processTasks.reserve(repo, 1)
	if reserveErr != nil {
		t.Fatalf("reserve: %v", reserveErr)
	}
	rec.Branch = "feat/adopted"
	rec.Path = "/ws/.sprout/tasks/adopted"
	processTasks.adopt(rec, "bg-task-7")

	got, detailErr := processTasks.snapshot(), error(nil)
	if detailErr != nil {
		t.Fatal(detailErr)
	}
	if len(got) != 1 || got[0].ID != "bg-task-7" {
		t.Fatalf("snapshot = %+v, want the adopted record under its ID", got)
	}
	if got[0].Branch != "feat/adopted" {
		t.Errorf("branch = %q, want feat/adopted", got[0].Branch)
	}
	if processTasks.get("bg-task-7") == nil {
		t.Error("adopted record is not reachable by ID")
	}
	if n := len(processTasks.pending); n != 0 {
		t.Errorf("pending placeholders = %d, want 0 after adopt", n)
	}
}

// A stop that lands while the runner goroutine is still finishing must not
// be overwritten: the background entry reports "stopped", so the task record
// must agree.
func TestTaskRegistry_SetTerminalPreservesStopped(t *testing.T) {
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	processTasks.add(&taskRecord{ID: "bg-task-race", Status: "running", Workspace: repo, Started: time.Now()})

	processTasks.markStopped("bg-task-race")
	processTasks.setTerminal("bg-task-race", "completed", "output", "abc1234")

	if got := processTasks.get("bg-task-race").Status; got != "stopped" {
		t.Errorf("status = %q, want stopped preserved", got)
	}
}

// A task must be able to ask a question when a browser is attached, and must
// fail fast (not block) when one is not. The old blanket SetWorkflowRun(true)
// made both cases fail outright.
func TestNewTaskAgent_KeepsWebUIApprovalSurface(t *testing.T) {
	repo := initTaskTestRepo(t)
	primary := newTaskTestAgent(t, repo)
	primary.SetEventMetadata(map[string]interface{}{"chat_id": "chat-1", "client_id": "client-1"})

	wt, err := createTaskWorktree(context.Background(), repo, "feat/ask", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { wt.remove(context.Background()) })

	taskAgent, err := newTaskAgent(primary, wt.Path, "bg-task-ask")
	if err != nil {
		t.Fatalf("newTaskAgent: %v", err)
	}

	if !taskAgent.IsTaskRun() {
		t.Error("a task must be marked as a task run so it keeps the WebUI approval surface")
	}
	if !taskAgent.IsWorkflowRun() {
		t.Error("a task must stay a workflow run so it never takes the primary's terminal")
	}
	if got := taskAgent.GetEventChatID(); got != "chat-1" {
		t.Errorf("chat id = %q, want the primary's chat so the dialog routes to the right tab", got)
	}
	if got := taskAgent.GetEventClientID(); got != "client-1" {
		t.Errorf("client id = %q, want the primary's client", got)
	}
	if taskAgent.GetSecurityApprovalMgr() != primary.GetSecurityApprovalMgr() {
		t.Error("a task must share the primary's approval manager so an answer reaches the asker")
	}
}

// Without a browser, a task's question fails immediately instead of blocking
// for the ask timeout.
func TestTaskAskUser_FailsFastWithoutSurface(t *testing.T) {
	repo := initTaskTestRepo(t)
	primary := newTaskTestAgent(t, repo)
	wt, err := createTaskWorktree(context.Background(), repo, "feat/no-surface", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { wt.remove(context.Background()) })

	taskAgent, err := newTaskAgent(primary, wt.Path, "bg-task-nosurface")
	if err != nil {
		t.Fatalf("newTaskAgent: %v", err)
	}
	// Drive the branch deterministically rather than skipping when a browser
	// happens to be attached.
	taskAgent.SetHasActiveWebUIClients(func() bool { return false })

	_, askErr := newAgentAskUserService(taskAgent).Ask(context.Background(), tools.AskUserRequest{Question: "which one?"})
	if askErr == nil {
		t.Fatal("expected an error when no surface can answer")
	}
	if !strings.Contains(askErr.Error(), "no interactive surface") {
		t.Errorf("error = %v, want the task no-surface message", askErr)
	}
}

// An interrupted task's worktree may hold the only copy of work it never
// committed. Merging its branch must not delete that.
func TestMergeTask_InterruptedKeepsWorktree(t *testing.T) {
	newTaskStateDir(t)
	resetProcessTasks(t)
	repo := initTaskTestRepo(t)
	a := newTaskTestAgent(t, repo)

	wt, err := createTaskWorktree(context.Background(), repo, "feat/interrupted", "")
	if err != nil {
		t.Fatalf("createTaskWorktree: %v", err)
	}
	t.Cleanup(func() { wt.remove(context.Background()) })

	// A committed change on the branch, plus uncommitted work that exists
	// only in the worktree.
	if writeErr := os.WriteFile(filepath.Join(wt.Path, "committed.txt"), []byte("c"), 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}
	if _, commitErr := commitTaskWork(context.Background(), wt); commitErr != nil {
		t.Fatalf("commitTaskWork: %v", commitErr)
	}
	uncommitted := filepath.Join(wt.Path, "uncommitted.txt")
	if writeErr := os.WriteFile(uncommitted, []byte("only here"), 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}

	// Simulate a restart: the record comes back from disk with no worktree
	// handle, marked interrupted.
	processTasks.add(&taskRecord{
		ID: "bg-task-int", Branch: wt.Branch, Path: wt.Path,
		Status: "running", Workspace: repo, Started: time.Now(),
	})
	processTasks.save(repo)
	resetProcessTasks(t)
	processTasks.loadTasks(repo)

	msg, mergeErr := a.MergeTask(context.Background(), "bg-task-int")
	if mergeErr != nil {
		t.Fatalf("MergeTask: %v", mergeErr)
	}
	if _, statErr := os.Stat(uncommitted); statErr != nil {
		t.Errorf("merging an interrupted task deleted its uncommitted work: %v", statErr)
	}
	if !strings.Contains(msg, "kept") {
		t.Errorf("merge message should say the worktree was kept: %q", msg)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "committed.txt")); statErr != nil {
		t.Errorf("the task's committed work was not merged: %v", statErr)
	}
}
