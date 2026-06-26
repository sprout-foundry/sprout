package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// lockRetryDelay bounds how often a TryLockContext call retries while
// waiting for the file lock. 100ms is fast enough that ctx cancellation
// is observed within the tool-timeout grace window, slow enough to avoid
// burning CPU when another process holds the lock for several seconds.
const lockRetryDelay = 100 * time.Millisecond

// Task represents a single task in the queue
type Task struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	Status       string    `json:"status"`   // pending, in_progress, completed, failed, blocked
	Priority     string    `json:"priority"` // high, medium, low
	AssignedTo   string    `json:"assigned_to,omitempty"`
	WorkingDir   string    `json:"working_dir,omitempty"`
	Persona      string    `json:"persona,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Result       string    `json:"result,omitempty"`
	ParentTaskID string    `json:"parent_task_id,omitempty"`
}

// SubtaskInput represents input for creating subtasks
type SubtaskInput struct {
	Title      string `json:"title"`
	WorkingDir string `json:"working_dir,omitempty"`
	Persona    string `json:"persona,omitempty"`
	Priority   string `json:"priority,omitempty"`
}

// Valid status values
var validStatuses = map[string]bool{
	"pending":     true,
	"in_progress": true,
	"completed":   true,
	"failed":      true,
	"blocked":     true,
}

// Valid priority values
var validPriorities = map[string]bool{
	"high":   true,
	"medium": true,
	"low":    true,
}

// Priority order for sorting (higher value = higher priority)
var priorityOrder = map[string]int{
	"high":   3,
	"medium": 2,
	"low":    1,
}

// TaskQueue manages a persistent file-based task queue.
//
// Public mutation methods (ReadTasks, PublishTask, AddTask) each acquire an
// exclusive or shared file lock, read the file fresh from disk, perform their
// operation, and write back atomically. This prevents the race condition that
// occurs when callers invoke Load() then PublishTask() as separate steps.
//
// Load() and Save() are retained as convenience wrappers for advanced usage.
type TaskQueue struct {
	filePath string
	tasks    []Task // in-memory cache used by Load()/Save() convenience methods
	mutex    sync.Mutex
	flock    *flock.Flock
}

// NewTaskQueue creates a new TaskQueue instance.
func NewTaskQueue(filePath string) *TaskQueue {
	return &TaskQueue{
		filePath: filePath,
		flock:    flock.New(filePath),
	}
}

// DefaultTaskQueuePath returns the default path for the task queue file.
func DefaultTaskQueuePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	return filepath.Join(homeDir, ".config", "sprout", "task_queue.json")
}

// ─── Private I/O helpers ───────────────────────────────────────────────────────

// ensureDir creates the parent directory for the queue file. Must be called
// before acquiring the flock because flock.Lock() tries to open() the file.
func (tq *TaskQueue) ensureDir() error {
	dir := filepath.Dir(tq.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

// readFromDisk reads and parses the task queue file. Returns an empty slice
// when the file is missing or empty. The caller must hold the appropriate lock.
func (tq *TaskQueue) readFromDisk() ([]Task, error) {
	if _, err := os.Stat(tq.filePath); os.IsNotExist(err) {
		return make([]Task, 0), nil
	}

	data, err := os.ReadFile(tq.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read task queue file: %w", err)
	}

	if len(data) == 0 {
		return make([]Task, 0), nil
	}

	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("failed to parse task queue: %w", err)
	}

	// Validate loaded tasks have minimum required fields and fix invalid data.
	for i := range tasks {
		if tasks[i].ID == "" {
			return nil, fmt.Errorf("task at index %d has empty ID (corrupt queue file)", i)
		}
		if tasks[i].Status == "" {
			return nil, fmt.Errorf("task %s at index %d has empty status (corrupt queue file)", tasks[i].ID, i)
		}
		if !validStatuses[tasks[i].Status] {
			return nil, fmt.Errorf("task %s has invalid status %q (corrupt queue file)", tasks[i].ID, tasks[i].Status)
		}
		// Default invalid/empty priorities to "medium" (benign migration fix)
		if tasks[i].Priority == "" || !validPriorities[tasks[i].Priority] {
			tasks[i].Priority = "medium"
		}
	}

	return tasks, nil
}

// writeToDisk marshals and atomically writes the task list. Uses a temporary
// file followed by rename. The caller must hold the write flock.
func (tq *TaskQueue) writeToDisk(tasks []Task) error {
	dir := filepath.Dir(tq.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tasks: %w", err)
	}

	tempFile := tq.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tempFile, tq.filePath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

// ─── Public convenience methods (backwards-compatible) ─────────────────────────

// Load reads tasks from disk into the in-memory cache. Useful for initial load
// followed by multiple operations before Save(), but prefer the self-contained
// ReadTasks / PublishTask / AddTask methods for safety.
func (tq *TaskQueue) Load(ctx context.Context) error {
	tq.mutex.Lock()
	defer tq.mutex.Unlock()

	if err := tq.ensureDir(); err != nil {
		return err
	}

	if _, err := tq.flock.TryRLockContext(ctx, lockRetryDelay); err != nil {
		return fmt.Errorf("failed to acquire read lock: %w", err)
	}
	defer tq.flock.Unlock()

	tasks, err := tq.readFromDisk()
	if err != nil {
		return err
	}
	tq.tasks = tasks
	return nil
}

// Save writes the in-memory cache back to disk atomically. Prefer
// PublishTask / AddTask for mutation since they handle the full atomic cycle.
func (tq *TaskQueue) Save(ctx context.Context) error {
	tq.mutex.Lock()
	defer tq.mutex.Unlock()

	if err := tq.ensureDir(); err != nil {
		return err
	}

	if _, err := tq.flock.TryLockContext(ctx, lockRetryDelay); err != nil {
		return fmt.Errorf("failed to acquire write lock: %w", err)
	}
	defer tq.flock.Unlock()

	return tq.writeToDisk(tq.tasks)
}

// ─── Self-contained atomic methods ─────────────────────────────────────────────

// ReadTasks reads tasks from disk, filtered by status, sorted by priority then
// created_at. Each call reads fresh from disk under a shared file lock, so it
// reflects the latest state without a prior Load().
//
// The shared lock is acquired via TryRLockContext so the caller's ctx
// cancels the wait (tool timeout / user interrupt) instead of blocking
// indefinitely when another process holds an exclusive write lock.
func (tq *TaskQueue) ReadTasks(ctx context.Context, status string, limit int) ([]Task, error) {
	tq.mutex.Lock()
	defer tq.mutex.Unlock()

	if err := tq.ensureDir(); err != nil {
		return nil, err
	}

	if _, err := tq.flock.TryRLockContext(ctx, lockRetryDelay); err != nil {
		return nil, fmt.Errorf("failed to acquire read lock: %w", err)
	}
	defer tq.flock.Unlock()

	tasks, err := tq.readFromDisk()
	if err != nil {
		return nil, err
	}

	// Filter by status
	var filtered []Task
	for _, task := range tasks {
		if status == "all" || task.Status == status {
			filtered = append(filtered, task)
		}
	}

	// Sort by priority (high > medium > low) then by created_at
	sort.Slice(filtered, func(i, j int) bool {
		priorityI := priorityOrder[filtered[i].Priority]
		priorityJ := priorityOrder[filtered[j].Priority]
		if priorityI != priorityJ {
			return priorityI > priorityJ
		}
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})

	// Apply limit (default 10)
	if limit <= 0 {
		limit = 10
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered, nil
}

// PublishTask atomically updates a task's status and result and optionally
// creates subtasks. Reads fresh from disk under an exclusive file lock, so it
// never overwrites work done by another process between Load and Publish.
//
// The exclusive lock is acquired via TryLockContext so the caller's ctx
// cancels the wait (tool timeout / user interrupt) instead of blocking
// indefinitely when another process holds the lock — historically the
// non-cancellable Lock() call orphaned a goroutine that kept the lock
// contended long after the tool's outer timeout fired.
func (tq *TaskQueue) PublishTask(ctx context.Context, taskID, status, result string, subtasks []SubtaskInput) ([]Task, error) {
	tq.mutex.Lock()
	defer tq.mutex.Unlock()

	// Validate status before acquiring the file lock
	if !validStatuses[status] {
		return nil, fmt.Errorf("invalid status: %s", status)
	}

	if err := tq.ensureDir(); err != nil {
		return nil, err
	}

	if _, err := tq.flock.TryLockContext(ctx, lockRetryDelay); err != nil {
		return nil, fmt.Errorf("failed to acquire write lock: %w", err)
	}
	defer tq.flock.Unlock()

	tasks, err := tq.readFromDisk()
	if err != nil {
		return nil, err
	}

	// Find and update the task
	var updatedTask *Task
	var taskIndex = -1
	for i := range tasks {
		if tasks[i].ID == taskID {
			taskIndex = i
			break
		}
	}
	if taskIndex == -1 {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	tasks[taskIndex].Status = status
	tasks[taskIndex].UpdatedAt = time.Now()
	if result != "" {
		tasks[taskIndex].Result = result
	}
	updatedTask = &tasks[taskIndex]

	// Create subtasks if provided
	var newSubtasks []Task
	for _, subtask := range subtasks {
		id, err := generateUUID()
		if err != nil {
			return nil, fmt.Errorf("failed to generate UUID for subtask: %w", err)
		}

		priority := subtask.Priority
		if priority == "" || !validPriorities[priority] {
			priority = "medium"
		}

		now := time.Now()
		newTask := Task{
			ID:           id,
			Title:        subtask.Title,
			Status:       "pending",
			Priority:     priority,
			WorkingDir:   subtask.WorkingDir,
			Persona:      subtask.Persona,
			CreatedAt:    now,
			UpdatedAt:    now,
			ParentTaskID: taskID,
		}
		tasks = append(tasks, newTask)
		newSubtasks = append(newSubtasks, newTask)
	}

	// Save atomically
	if err := tq.writeToDisk(tasks); err != nil {
		return nil, fmt.Errorf("failed to save task queue: %w", err)
	}

	// Return updated task and new subtasks
	resultTasks := []Task{*updatedTask}
	resultTasks = append(resultTasks, newSubtasks...)
	return resultTasks, nil
}

// AddTask atomically adds a new task. Reads fresh from disk under an exclusive
// file lock, so concurrent adds from separate processes do not overwrite each
// other.
//
// The exclusive lock is acquired via TryLockContext so the caller's ctx
// cancels the wait when another process holds the lock.
func (tq *TaskQueue) AddTask(ctx context.Context, title, description, priority, workingDir, persona string) (*Task, error) {
	tq.mutex.Lock()
	defer tq.mutex.Unlock()

	if priority == "" || !validPriorities[priority] {
		priority = "medium"
	}

	if err := tq.ensureDir(); err != nil {
		return nil, err
	}

	if _, err := tq.flock.TryLockContext(ctx, lockRetryDelay); err != nil {
		return nil, fmt.Errorf("failed to acquire write lock: %w", err)
	}
	defer tq.flock.Unlock()

	tasks, err := tq.readFromDisk()
	if err != nil {
		return nil, err
	}

	id, err := generateUUID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate UUID: %w", err)
	}

	now := time.Now()
	task := Task{
		ID:          id,
		Title:       title,
		Description: description,
		Status:      "pending",
		Priority:    priority,
		WorkingDir:  workingDir,
		Persona:     persona,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	tasks = append(tasks, task)

	if err := tq.writeToDisk(tasks); err != nil {
		return nil, fmt.Errorf("failed to save task queue: %w", err)
	}

	return &task, nil
}

// generateUUID generates a UUID-like identifier using crypto/rand.
// Returns a 32-character hex string (e.g., "a1b2c3d4e5f67890abcdef1234567890").
func generateUUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
