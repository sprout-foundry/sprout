package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTaskStateDir points the state dir at a temp directory for the test.
func newTaskStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", dir)
	return dir
}

func TestTaskRegistry_SaveAndLoad(t *testing.T) {
	newTaskStateDir(t)
	resetProcessTasks(t)
	repo := t.TempDir()

	processTasks.add(&taskRecord{
		ID: "bg-task-1", Branch: "feat/add-login", Path: repo + "/.sprout/tasks/add-login",
		Prompt: "add login", Status: "completed", Commit: "abc1234",
		Summary: "done", Workspace: repo, Started: time.Now().Add(-time.Minute),
	})
	processTasks.setTerminal("bg-task-1", "completed", "done", "abc1234")

	// A fresh process sees the record.
	resetProcessTasks(t)
	processTasks.loadTasks(repo)

	info, err := processTasks.snapshot(), error(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 1 {
		t.Fatalf("loaded %d records, want 1", len(info))
	}
	got := info[0]
	if got.ID != "bg-task-1" || got.Branch != "feat/add-login" {
		t.Errorf("loaded %+v, want the saved record", got)
	}
	if got.Status != "completed" {
		t.Errorf("status = %q, want completed", got.Status)
	}
	if got.Commit != "abc1234" {
		t.Errorf("commit = %q, want abc1234", got.Commit)
	}
}

// A task cannot outlive its process, so a record saved as running must come
// back as interrupted — never as running, which the concurrency limit and
// /task merge would act on.
func TestTaskRegistry_LoadMarksRunningAsInterrupted(t *testing.T) {
	newTaskStateDir(t)
	resetProcessTasks(t)
	repo := t.TempDir()

	processTasks.add(&taskRecord{
		ID: "bg-task-live", Branch: "feat/live", Path: repo + "/wt",
		Status: "running", Workspace: repo, Started: time.Now(),
	})
	processTasks.save(repo)

	resetProcessTasks(t)
	processTasks.loadTasks(repo)

	rec := processTasks.get("bg-task-live")
	if rec == nil {
		t.Fatal("record was not restored")
	}
	if rec.Status != "interrupted" {
		t.Errorf("status = %q, want interrupted", rec.Status)
	}
	if rec.Finished.IsZero() {
		t.Error("an interrupted record needs a finish time so its elapsed time stops growing")
	}
}

// An interrupted task must not occupy a concurrency slot: nothing is running.
func TestTaskRegistry_InterruptedDoesNotHoldSlot(t *testing.T) {
	newTaskStateDir(t)
	resetProcessTasks(t)
	repo := t.TempDir()

	processTasks.add(&taskRecord{ID: "bg-task-x", Status: "running", Workspace: repo, Started: time.Now()})
	processTasks.save(repo)
	resetProcessTasks(t)
	processTasks.loadTasks(repo)

	if _, reserveErr := processTasks.reserve(repo, 1); reserveErr != nil {
		t.Errorf("an interrupted task should not hold a slot: %v", reserveErr)
	}
}

func TestTaskRegistry_LoadIgnoresCorruptFile(t *testing.T) {
	stateDir := newTaskStateDir(t)
	resetProcessTasks(t)
	repo := t.TempDir()

	path, err := taskStatePath(repo)
	if err != nil {
		t.Fatalf("taskStatePath: %v", err)
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}
	if writeErr := os.WriteFile(path, []byte("{not json"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	processTasks.loadTasks(repo) // must not panic
	if got := len(processTasks.snapshot()); got != 0 {
		t.Errorf("loaded %d records from a corrupt file, want 0", got)
	}
	if _, statErr := os.Stat(stateDir); statErr != nil {
		t.Errorf("state dir missing: %v", statErr)
	}
}

// Two workspaces must not share a task list.
func TestTaskRegistry_SaveIsPerWorkspace(t *testing.T) {
	newTaskStateDir(t)
	resetProcessTasks(t)
	repoA := t.TempDir()
	repoB := t.TempDir()

	processTasks.add(&taskRecord{ID: "bg-task-a", Status: "completed", Workspace: repoA, Started: time.Now(), Branch: "feat/a"})
	processTasks.add(&taskRecord{ID: "bg-task-b", Status: "completed", Workspace: repoB, Started: time.Now(), Branch: "feat/b"})
	processTasks.save(repoA)
	processTasks.save(repoB)

	resetProcessTasks(t)
	processTasks.loadTasks(repoA)

	got := processTasks.snapshot()
	if len(got) != 1 || got[0].ID != "bg-task-a" {
		t.Errorf("workspace A loaded %+v, want only its own task", got)
	}
}

func TestTaskStatePath_IsFilesystemSafe(t *testing.T) {
	newTaskStateDir(t)
	path, err := taskStatePath("/tmp/weird path/with:colons/and/slashes")
	if err != nil {
		t.Fatalf("taskStatePath: %v", err)
	}
	base := filepath.Base(path)
	if strings.ContainsAny(base, "/\\:") {
		t.Errorf("state filename %q contains a path separator or colon", base)
	}
	if !strings.HasSuffix(base, ".json") {
		t.Errorf("state filename %q does not end in .json", base)
	}
}

func TestTaskInterruptedNote(t *testing.T) {
	note := taskInterruptedNote(TaskInfo{ID: "bg-task-1", Status: "interrupted", Branch: "feat/x", Worktree: "/ws/wt"})
	for _, want := range []string{"bg-task-1", "feat/x", "/ws/wt"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q: %s", want, note)
		}
	}
	if got := taskInterruptedNote(TaskInfo{ID: "bg-task-2", Status: "completed"}); got != "" {
		t.Errorf("completed task produced a note: %q", got)
	}
}

// The sanitized prefix is lossy (/a/b-c and /a/b_c both become a_b_c), so the
// key must carry a hash or two workspaces share one state file and destroy
// each other's tasks.
func TestWorkspaceKey_DistinguishesSimilarPaths(t *testing.T) {
	keys := map[string]string{
		"/a/b-c": workspaceKey("/a/b-c"),
		"/a/b_c": workspaceKey("/a/b_c"),
		"/a/b/c": workspaceKey("/a/b/c"),
	}
	seen := map[string]string{}
	for path, key := range keys {
		if other, dup := seen[key]; dup {
			t.Errorf("%q and %q share the state key %q", path, other, key)
		}
		seen[key] = path
	}
}

func TestWorkspaceKey_Stable(t *testing.T) {
	if workspaceKey("/a/b") != workspaceKey("/a/b/") {
		t.Error("the same workspace must produce the same key regardless of a trailing separator")
	}
	first := workspaceKey("/a/b")
	for i := 0; i < 5; i++ {
		if got := workspaceKey("/a/b"); got != first {
			t.Fatalf("workspaceKey is not deterministic: %q then %q", first, got)
		}
	}
}

// A record whose stored workspace differs from the file it came from must
// still belong to the workspace being loaded: otherwise the next save sees
// zero records and deletes the file.
func TestTaskRegistry_LoadScopesRecordsToRequestedWorkspace(t *testing.T) {
	newTaskStateDir(t)
	resetProcessTasks(t)
	repo := t.TempDir()

	processTasks.add(&taskRecord{
		ID: "bg-task-stale", Branch: "feat/x", Path: repo + "/wt",
		Status: "completed", Workspace: "/somewhere/else", Started: time.Now(),
	})
	processTasks.save("/somewhere/else")

	resetProcessTasks(t)
	processTasks.loadTasks(repo)

	// The record was written under another workspace's file, so this
	// workspace has nothing — and saving must not delete anything it owns.
	if got := len(processTasks.snapshot()); got != 0 {
		t.Errorf("loaded %d records from another workspace's file, want 0", got)
	}
}

// Two tasks finishing at once must both reach disk.
func TestTaskRegistry_ConcurrentSavesKeepEveryRecord(t *testing.T) {
	newTaskStateDir(t)
	resetProcessTasks(t)
	repo := t.TempDir()

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("bg-task-%d", i)
		processTasks.add(&taskRecord{ID: id, Status: "running", Workspace: repo, Started: time.Now(), Branch: "feat/" + id})
		wg.Add(1)
		go func() {
			defer wg.Done()
			processTasks.setTerminal(id, "completed", "", "")
		}()
	}
	wg.Wait()

	resetProcessTasks(t)
	processTasks.loadTasks(repo)

	if got := len(processTasks.snapshot()); got != n {
		t.Errorf("loaded %d records, want %d — a concurrent save dropped one", got, n)
	}
}
