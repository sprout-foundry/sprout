//go:build !js

package cmd

import (
	"strings"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// =============================================================================
// buildQueueTaskQuery
// =============================================================================

func TestBuildQueueTaskQuery_BasicTask(t *testing.T) {
	task := tools.Task{
		ID:        "test-123",
		Title:     "Fix the login bug",
		Priority:  "high",
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	query := buildQueueTaskQuery(task)

	// Must contain key fields
	if !strings.Contains(query, "Fix the login bug") {
		t.Errorf("query should contain task title; got:\n%s", query)
	}
	if !strings.Contains(query, "test-123") {
		t.Errorf("query should contain task ID; got:\n%s", query)
	}
	if !strings.Contains(query, "high") {
		t.Errorf("query should contain priority; got:\n%s", query)
	}
	if !strings.Contains(query, "run_subagent") {
		t.Errorf("query should mention run_subagent; got:\n%s", query)
	}
	if !strings.Contains(query, "task_queue_publish") {
		t.Errorf("query should mention task_queue_publish; got:\n%s", query)
	}
}

func TestBuildQueueTaskQuery_WithDescription(t *testing.T) {
	task := tools.Task{
		ID:          "test-456",
		Title:       "Refactor auth module",
		Description: "The auth module needs to be split into smaller components",
		Priority:    "medium",
		Status:      "pending",
		CreatedAt:   time.Now(),
	}

	query := buildQueueTaskQuery(task)

	if !strings.Contains(query, "The auth module needs to be split") {
		t.Errorf("query should contain description; got:\n%s", query)
	}
	if !strings.Contains(query, "Refactor auth module") {
		t.Errorf("query should contain title; got:\n%s", query)
	}
}

func TestBuildQueueTaskQuery_WithWorkingDir(t *testing.T) {
	task := tools.Task{
		ID:         "test-789",
		Title:      "Add tests",
		Priority:   "low",
		WorkingDir: "/home/user/project",
		Status:     "pending",
		CreatedAt:  time.Now(),
	}

	query := buildQueueTaskQuery(task)

	if !strings.Contains(query, "/home/user/project") {
		t.Errorf("query should contain working directory; got:\n%s", query)
	}
	if !strings.Contains(query, "Working directory") {
		t.Errorf("query should label the working directory; got:\n%s", query)
	}
}

func TestBuildQueueTaskQuery_WithPersona(t *testing.T) {
	task := tools.Task{
		ID:        "test-persona",
		Title:     "Write unit tests",
		Priority:  "high",
		Persona:   "tester",
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	query := buildQueueTaskQuery(task)

	if !strings.Contains(query, "tester") {
		t.Errorf("query should contain persona; got:\n%s", query)
	}
	if !strings.Contains(query, "Recommended persona") {
		t.Errorf("query should label recommended persona; got:\n%s", query)
	}
}

func TestBuildQueueTaskQuery_WithParentTask(t *testing.T) {
	task := tools.Task{
		ID:           "test-sub",
		Title:        "Implement subtask",
		Priority:     "medium",
		ParentTaskID: "test-parent",
		Status:       "pending",
		CreatedAt:    time.Now(),
	}

	query := buildQueueTaskQuery(task)

	if !strings.Contains(query, "test-parent") {
		t.Errorf("query should contain parent task ID; got:\n%s", query)
	}
	if !strings.Contains(query, "Parent task") {
		t.Errorf("query should label the parent task; got:\n%s", query)
	}
}

func TestBuildQueueTaskQuery_FullTask(t *testing.T) {
	task := tools.Task{
		ID:          "full-task-1",
		Title:       "Full task test",
		Description: "This is a full task with all fields",
		Priority:    "high",
		Persona:     "coder",
		WorkingDir:  "/tmp/test",
		Status:      "pending",
		CreatedAt:   time.Now(),
	}

	query := buildQueueTaskQuery(task)

	// Check all fields appear
	expectedFields := []string{
		"Full task test",
		"full-task-1",
		"This is a full task with all fields",
		"/tmp/test",
		"coder",
		"high",
		"run_subagent",
		"task_queue_publish",
		"Recommended persona: coder",
	}

	for _, field := range expectedFields {
		if !strings.Contains(query, field) {
			t.Errorf("query should contain %q; got:\n%s", field, query)
		}
	}
}

func TestBuildQueueTaskQuery_EmptyDescriptionAndFields(t *testing.T) {
	task := tools.Task{
		ID:       "minimal-task",
		Title:    "Minimal task",
		Priority: "medium",
		Status:   "pending",
	}

	query := buildQueueTaskQuery(task)

	// Should still contain title, ID, priority, and instructions
	if !strings.Contains(query, "Minimal task") {
		t.Errorf("query should contain title; got:\n%s", query)
	}
	if !strings.Contains(query, "minimal-task") {
		t.Errorf("query should contain ID; got:\n%s", query)
	}

	// Should NOT contain optional fields when empty
	if strings.Contains(query, "Working directory:") {
		t.Error("query should not contain working directory label when empty")
	}
	if strings.Contains(query, "Persona:") {
		t.Error("query should not contain persona label when empty")
	}
	if strings.Contains(query, "Parent task:") {
		t.Error("query should not contain parent task label when empty")
	}
	if strings.Contains(query, "Recommended persona:") {
		t.Error("query should not contain recommended persona when persona is empty")
	}
}

func TestBuildQueueTaskQuery_OutputIsMultiline(t *testing.T) {
	task := tools.Task{
		ID:          "multiline-test",
		Title:       "Multi-line test",
		Description: "With description",
		Priority:    "high",
		Persona:     "debugger",
		WorkingDir:  "/tmp/work",
		Status:      "pending",
	}

	query := buildQueueTaskQuery(task)

	lines := strings.Split(query, "\n")
	if len(lines) < 5 {
		t.Errorf("expected at least 5 lines in query, got %d:\n%s", len(lines), query)
	}
}

func TestBuildQueueTaskQuery_StartsCorrectly(t *testing.T) {
	task := tools.Task{
		ID:       "start-test",
		Title:    "Start test",
		Priority: "medium",
		Status:   "pending",
	}

	query := buildQueueTaskQuery(task)

	if !strings.HasPrefix(query, "Process queued task: Start test") {
		t.Errorf("query should start with 'Process queued task: <title>'; got:\n%s", query)
	}
}
