package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper: create a TaskQueue backed by a temp file.
func newTestTaskQueue(t *testing.T) *TaskQueue {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	return NewTaskQueue(path)
}

// Helper: write raw JSON to the queue file (bypasses locking).
func writeRawQueue(t *testing.T, tq *TaskQueue, tasks []Task) {
	t.Helper()
	data, err := json.MarshalIndent(tasks, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(tq.filePath), 0755))
	require.NoError(t, os.WriteFile(tq.filePath, data, 0644))
}

// ─── Construction ────────────────────────────────────────────────────────────────

func TestNewTaskQueue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	tq := NewTaskQueue(path)
	require.NotNil(t, tq)
	assert.Equal(t, path, tq.filePath)
}

func TestDefaultTaskQueuePath(t *testing.T) {
	path := DefaultTaskQueuePath()
	assert.Contains(t, path, "task_queue.json")
}

// ─── AddTask ─────────────────────────────────────────────────────────────────────

func TestAddTask(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"Build the robot", "Assemble parts", "high", "/tmp/build", "engineer")
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.Equal(t, "Build the robot", task.Title)
	assert.Equal(t, "Assemble parts", task.Description)
	assert.Equal(t, "high", task.Priority)
	assert.Equal(t, "pending", task.Status)
	assert.Equal(t, "/tmp/build", task.WorkingDir)
	assert.Equal(t, "engineer", task.Persona)
	assert.NotEmpty(t, task.ID)
	assert.True(t, task.CreatedAt.After(time.Time{}))

	// Verify persisted to disk
	tq2 := NewTaskQueue(tq.filePath)
	tasks, err := tq2.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, task.ID, tasks[0].ID)
	assert.Equal(t, task.Title, tasks[0].Title)
}

func TestAddTaskDefaultPriority(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"Check logs", "", "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "medium", task.Priority)
}

func TestAddTaskInvalidPriority(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"Deploy", "", "ultra", "", "")
	require.NoError(t, err)
	assert.Equal(t, "medium", task.Priority)
}

func TestAddTaskMultiple(t *testing.T) {
	tq := newTestTaskQueue(t)
	_, err := tq.AddTask(context.Background(),"Task A", "", "high", "", "")
	require.NoError(t, err)
	_, err = tq.AddTask(context.Background(),"Task B", "", "low", "", "")
	require.NoError(t, err)

	tasks, err := tq.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 2)
}

// ─── ReadTasks ───────────────────────────────────────────────────────────────────

func TestReadTasksEmpty(t *testing.T) {
	tq := newTestTaskQueue(t)
	tasks, err := tq.ReadTasks(context.Background(),"pending", 10)
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestReadTasksByStatus(t *testing.T) {
	tq := newTestTaskQueue(t)
	tq.AddTask(context.Background(),"Pending work", "", "medium", "", "")
	tq.AddTask(context.Background(),"Done work", "", "medium", "", "")
	writeRawQueue(t, tq, []Task{
		{ID: "a", Title: "Pending work", Status: "pending", Priority: "medium", CreatedAt: time.Now()},
		{ID: "b", Title: "Done work", Status: "completed", Priority: "medium", CreatedAt: time.Now()},
	})

	tasks, err := tq.ReadTasks(context.Background(),"pending", 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "Pending work", tasks[0].Title)
}

func TestReadTasksAll(t *testing.T) {
	tq := newTestTaskQueue(t)
	writeRawQueue(t, tq, []Task{
		{ID: "a", Title: "P", Status: "pending", Priority: "medium", CreatedAt: time.Now()},
		{ID: "b", Title: "C", Status: "completed", Priority: "medium", CreatedAt: time.Now()},
		{ID: "c", Title: "B", Status: "blocked", Priority: "medium", CreatedAt: time.Now()},
	})

	tasks, err := tq.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 3)
}

func TestReadTasksLimit(t *testing.T) {
	tq := newTestTaskQueue(t)
	tasksRaw := make([]Task, 15)
	for i := range tasksRaw {
		tasksRaw[i] = Task{ID: string(rune('A' + i)), Title: "t", Status: "pending", Priority: "medium", CreatedAt: time.Now()}
	}
	writeRawQueue(t, tq, tasksRaw)

	tasks, err := tq.ReadTasks(context.Background(),"all", 5)
	require.NoError(t, err)
	assert.Len(t, tasks, 5)
}

func TestReadTasksDefaultLimit(t *testing.T) {
	tq := newTestTaskQueue(t)
	tasksRaw := make([]Task, 15)
	for i := range tasksRaw {
		tasksRaw[i] = Task{ID: string(rune('A' + i)), Title: "t", Status: "pending", Priority: "medium", CreatedAt: time.Now()}
	}
	writeRawQueue(t, tq, tasksRaw)

	tasks, err := tq.ReadTasks(context.Background(),"all", 0)
	require.NoError(t, err)
	assert.Len(t, tasks, 10) // default limit
}

func TestReadTasksPrioritySort(t *testing.T) {
	tq := newTestTaskQueue(t)
	writeRawQueue(t, tq, []Task{
		{ID: "low", Title: "low", Status: "pending", Priority: "low", CreatedAt: time.Now()},
		{ID: "high", Title: "high", Status: "pending", Priority: "high", CreatedAt: time.Now()},
		{ID: "medium", Title: "medium", Status: "pending", Priority: "medium", CreatedAt: time.Now()},
	})

	tasks, err := tq.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 3)
	assert.Equal(t, "high", tasks[0].Priority)
	assert.Equal(t, "medium", tasks[1].Priority)
	assert.Equal(t, "low", tasks[2].Priority)
}

func TestReadTasksSortByCreatedAtWithinSamePriority(t *testing.T) {
	tq := newTestTaskQueue(t)
	base := time.Now()
	writeRawQueue(t, tq, []Task{
		{ID: "b", Title: "later", Status: "pending", Priority: "medium", CreatedAt: base.Add(1 * time.Second)},
		{ID: "a", Title: "earlier", Status: "pending", Priority: "medium", CreatedAt: base},
	})

	tasks, err := tq.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Equal(t, "a", tasks[0].ID) // earlier first
	assert.Equal(t, "b", tasks[1].ID)
}

// ─── PublishTask ─────────────────────────────────────────────────────────────────

func TestPublishTaskUpdateStatus(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"My task", "", "medium", "", "")
	require.NoError(t, err)

	result, err := tq.PublishTask(context.Background(),task.ID, "completed", "", nil)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "completed", result[0].Status)

	// Verify persisted
	tasks, err := tq.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Equal(t, "completed", tasks[0].Status)
}

func TestPublishTaskWithResult(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"Test", "", "medium", "", "")
	require.NoError(t, err)

	result, err := tq.PublishTask(context.Background(),task.ID, "completed", "All tests passed", nil)
	require.NoError(t, err)
	assert.Equal(t, "All tests passed", result[0].Result)
}

func TestPublishTaskNotFound(t *testing.T) {
	tq := newTestTaskQueue(t)
	_, err := tq.PublishTask(context.Background(),"nonexistent", "completed", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPublishTaskInvalidStatus(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"Test", "", "medium", "", "")
	require.NoError(t, err)
	_, err = tq.PublishTask(context.Background(),task.ID, "banana", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid status")
}

func TestPublishTaskWithSubtasks(t *testing.T) {
	tq := newTestTaskQueue(t)
	parent, err := tq.AddTask(context.Background(),"Parent task", "", "high", "", "")
	require.NoError(t, err)

	result, err := tq.PublishTask(context.Background(),parent.ID, "in_progress", "", []SubtaskInput{
		{Title: "Sub 1", Priority: "high"},
		{Title: "Sub 2", Priority: "low"},
	})
	require.NoError(t, err)
	assert.Len(t, result, 3) // parent + 2 subtasks
	assert.Equal(t, parent.ID, result[0].ID)
	assert.Equal(t, "Sub 1", result[1].Title)
	assert.Equal(t, "Sub 2", result[2].Title)
	assert.Equal(t, "pending", result[1].Status)
	assert.Equal(t, "pending", result[2].Status)
}

func TestSubtasksInheritParent(t *testing.T) {
	tq := newTestTaskQueue(t)
	parent, err := tq.AddTask(context.Background(),"Parent task", "", "high", "", "")
	require.NoError(t, err)

	result, err := tq.PublishTask(context.Background(),parent.ID, "in_progress", "", []SubtaskInput{
		{Title: "Child", WorkingDir: "/sub", Persona: "worker"},
	})
	require.NoError(t, err)
	assert.Equal(t, parent.ID, result[1].ParentTaskID)
	assert.Equal(t, "/sub", result[1].WorkingDir)
	assert.Equal(t, "worker", result[1].Persona)
}

func TestPublishTaskSubtaskDefaultPriority(t *testing.T) {
	tq := newTestTaskQueue(t)
	parent, err := tq.AddTask(context.Background(),"Parent", "", "high", "", "")
	require.NoError(t, err)

	result, err := tq.PublishTask(context.Background(),parent.ID, "in_progress", "", []SubtaskInput{
		{Title: "Child", Priority: "invalid_priority"},
	})
	require.NoError(t, err)
	assert.Equal(t, "medium", result[1].Priority)
}

// ─── Atomic Write ────────────────────────────────────────────────────────────────

func TestAtomicWrite(t *testing.T) {
	tq := newTestTaskQueue(t)
	_, err := tq.AddTask(context.Background(),"Task 1", "", "medium", "", "")
	require.NoError(t, err)

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(tq.filePath)
	require.NoError(t, err)
	var tasks []Task
	require.NoError(t, json.Unmarshal(data, &tasks))
	assert.Len(t, tasks, 1)

	// Temp file should NOT exist after successful write
	_, err = os.Stat(tq.filePath + ".tmp")
	assert.True(t, os.IsNotExist(err), "temp file should be cleaned up after atomic write")
}

// ─── Edge Cases ──────────────────────────────────────────────────────────────────

func TestEmptyFileHandling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))

	tq := NewTaskQueue(path)
	tasks, err := tq.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestMissingFileHandling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json") // does not exist yet

	tq := NewTaskQueue(path)
	tasks, err := tq.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestLoadThenSave(t *testing.T) {
	// Test backward-compatible Load/Save cycle
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	require.NoError(t, os.WriteFile(path, []byte(`[]`), 0644))

	tq := NewTaskQueue(path)
	err := tq.Load(context.Background())
	require.NoError(t, err)
	err = tq.Save(context.Background())
	require.NoError(t, err)
}

func TestCorruptFileEmptyID(t *testing.T) {
	tq := newTestTaskQueue(t)
	writeRawQueue(t, tq, []Task{
		{ID: "", Title: "Bad", Status: "pending", Priority: "medium", CreatedAt: time.Now()},
	})
	_, err := tq.ReadTasks(context.Background(),"all", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty ID")
}

func TestCorruptFileEmptyStatus(t *testing.T) {
	tq := newTestTaskQueue(t)
	writeRawQueue(t, tq, []Task{
		{ID: "abc", Title: "Bad", Status: "", Priority: "medium", CreatedAt: time.Now()},
	})
	_, err := tq.ReadTasks(context.Background(),"all", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty status")
}

func TestCorruptFileInvalidStatus(t *testing.T) {
	tq := newTestTaskQueue(t)
	writeRawQueue(t, tq, []Task{
		{ID: "abc", Title: "Bad", Status: "banana", Priority: "medium", CreatedAt: time.Now()},
	})
	_, err := tq.ReadTasks(context.Background(),"all", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid status")
}

func TestInvalidPriorityOnRead(t *testing.T) {
	tq := newTestTaskQueue(t)
	writeRawQueue(t, tq, []Task{
		{ID: "abc", Title: "Fix me", Status: "pending", Priority: "urgent", CreatedAt: time.Now()},
	})
	tasks, err := tq.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "medium", tasks[0].Priority) // defaulted
}

func TestAddTaskCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "deep", "queue.json") // dir doesn't exist
	tq := NewTaskQueue(path)
	task, err := tq.AddTask(context.Background(),"Test", "", "medium", "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, task.ID)
}

func TestPublishTaskCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "queue.json")
	tq := NewTaskQueue(path)
	// First add a task to the non-existent file
	task, err := tq.AddTask(context.Background(),"Base", "", "medium", "", "")
	require.NoError(t, err)
	// Then publish (should also create dir if needed)
	result, err := tq.PublishTask(context.Background(),task.ID, "completed", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "completed", result[0].Status)
}

// ─── Concurrent Access ───────────────────────────────────────────────────────────

func TestConcurrentAdd(t *testing.T) {
	tq := newTestTaskQueue(t)
	const goroutines = 20
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Use the SAME TaskQueue instance — the sync.Mutex handles in-process safety.
			// File locking (flock) handles cross-process safety only.
			_, err := tq.AddTask(context.Background(),"concurrent task", "", "medium", "", "")
			errCh <- err
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.NoError(t, err)
	}

	// All tasks should be present (no lost writes)
	tasks, err := tq.ReadTasks(context.Background(),"all", 100)
	require.NoError(t, err)
	assert.Len(t, tasks, goroutines, "all concurrent adds should be persisted without data loss")
}

func TestConcurrentAddAndRead(t *testing.T) {
	tq := newTestTaskQueue(t)
	const adds = 10
	var wg sync.WaitGroup
	errCh := make(chan error, adds*2)

	for i := 0; i < adds; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := tq.AddTask(context.Background(),"concurrent task", "", "medium", "", "")
			errCh <- err
		}()
		go func() {
			defer wg.Done()
			_, err := tq.ReadTasks(context.Background(),"all", 100)
			errCh <- err
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.NoError(t, err)
	}

	// All adds should have succeeded
	tasks, err := tq.ReadTasks(context.Background(),"all", 100)
	require.NoError(t, err)
	assert.Len(t, tasks, adds, "all concurrent adds should survive concurrent reads")
}

func TestConcurrentPublishAndRead(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"Base task", "", "medium", "", "")
	require.NoError(t, err)

	const writers = 5
	var wg sync.WaitGroup
	errCh := make(chan error, writers*2)

	statuses := []string{"in_progress", "completed", "failed", "blocked", "pending"}
	for i := 0; i < writers; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			_, err := tq.PublishTask(context.Background(),task.ID, statuses[idx], "", nil)
			errCh <- err
		}(i)
		go func() {
			defer wg.Done()
			_, err := tq.ReadTasks(context.Background(),"all", 100)
			errCh <- err
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.NoError(t, err)
	}

	// Final state: last writer wins (that's fine—mutex serializes, no corruption)
	tasks, err := tq.ReadTasks(context.Background(),"all", 100)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
}

// ─── Full Round-Trip (no Load() required) ────────────────────────────────────────

func TestAddThenReadWithoutLoad(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"First", "", "high", "", "")
	require.NoError(t, err)

	// A brand-new TaskQueue instance should see the data without calling Load()
	tq2 := NewTaskQueue(tq.filePath)
	tasks, err := tq2.ReadTasks(context.Background(),"all", 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, task.ID, tasks[0].ID)
}

func TestAddThenPublishWithoutLoad(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(),"First", "", "high", "", "")
	require.NoError(t, err)

	// A brand-new TaskQueue instance should publish without calling Load()
	tq2 := NewTaskQueue(tq.filePath)
	result, err := tq2.PublishTask(context.Background(),task.ID, "completed", "done", nil)
	require.NoError(t, err)
	assert.Equal(t, "completed", result[0].Status)
	assert.Equal(t, "done", result[0].Result)
}

// ─── Context cancellation ────────────────────────────────────────────────────────

// TestPublishTaskRespectsContextCancellation verifies that a PublishTask call
// blocked waiting for the exclusive flock unblocks promptly when its context
// is canceled, instead of orphaning the goroutine. This is the regression
// guard for the "task_queue (publish) hangs for 30+ minutes" failure mode
// where the outer tool timeout fired but the inner goroutine stayed wedged
// on the non-cancellable flock.Lock() call.
func TestPublishTaskRespectsContextCancellation(t *testing.T) {
	tq := newTestTaskQueue(t)
	task, err := tq.AddTask(context.Background(), "Locked", "", "medium", "", "")
	require.NoError(t, err)

	// Hold the exclusive lock from a separate Flock instance so the next
	// PublishTask has to wait — mimics another process owning the lock.
	tqHolder := NewTaskQueue(tq.filePath)
	require.NoError(t, tqHolder.flock.Lock())
	defer func() { _ = tqHolder.flock.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tq.PublishTask(ctx, task.ID, "completed", "", nil)
		done <- err
	}()

	// Cancel the context shortly after PublishTask starts waiting.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err, "PublishTask should return an error after ctx cancellation")
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("PublishTask did not return within 2s of ctx cancellation — flock wait is not respecting context")
	}
}

// TestReadTasksRespectsContextCancellation is the read-path counterpart:
// a shared-lock acquisition should also unblock on ctx cancellation when
// another holder has the exclusive lock.
func TestReadTasksRespectsContextCancellation(t *testing.T) {
	tq := newTestTaskQueue(t)
	_, err := tq.AddTask(context.Background(), "Locked", "", "medium", "", "")
	require.NoError(t, err)

	tqHolder := NewTaskQueue(tq.filePath)
	require.NoError(t, tqHolder.flock.Lock())
	defer func() { _ = tqHolder.flock.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tq.ReadTasks(ctx, "all", 10)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err, "ReadTasks should return an error after ctx cancellation")
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("ReadTasks did not return within 2s of ctx cancellation — flock wait is not respecting context")
	}
}
