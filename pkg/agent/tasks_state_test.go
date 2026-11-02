package agent

import (
	"os"
	"path/filepath"
	"strings"
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
