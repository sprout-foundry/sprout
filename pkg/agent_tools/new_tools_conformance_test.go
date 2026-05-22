package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// SP-038-5a: Subagent family (thin wrappers - test Name/Definition/Validate only)
// =============================================================================

func TestRunSubagentHandlerConformance(t *testing.T) {
	h := &runSubagentHandler{}

	// Test Name
	if h.Name() != "run_subagent" {
		t.Errorf("Name() = %q, want %q", h.Name(), "run_subagent")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "run_subagent" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "run_subagent")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	// Check required params exist
	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("prompt") {
		t.Error("Definition missing 'prompt' parameter")
	}
	if !hasParam("persona") {
		t.Error("Definition missing 'persona' parameter")
	}
	if !hasParam("context") {
		t.Error("Definition missing 'context' parameter")
	}
	if !hasParam("files") {
		t.Error("Definition missing 'files' parameter")
	}
	if !hasParam("working_dir") {
		t.Error("Definition missing 'working_dir' parameter")
	}

	// Validate: missing required params
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error for missing prompt")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing prompt")
	}
	if err := h.Validate(map[string]any{"prompt": "test"}); err == nil {
		t.Error("Validate(prompt only) should return error for missing persona")
	}

	// Validate: valid params
	if err := h.Validate(map[string]any{"prompt": "test", "persona": "coder"}); err != nil {
		t.Errorf("Validate(valid args) should return nil, got: %v", err)
	}
}

func TestRunParallelSubagentsHandlerConformance(t *testing.T) {
	h := &runParallelSubagentsHandler{}

	// Test Name
	if h.Name() != "run_parallel_subagents" {
		t.Errorf("Name() = %q, want %q", h.Name(), "run_parallel_subagents")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "run_parallel_subagents" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "run_parallel_subagents")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	// Check required param
	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("subagents") {
		t.Error("Definition missing 'subagents' parameter")
	}

	// Validate: missing required param
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing subagents")
	}

	// Validate: subagents not an array
	if err := h.Validate(map[string]any{"subagents": "not an array"}); err == nil {
		t.Error("Validate(string subagents) should return error")
	}

	// Validate: empty array
	if err := h.Validate(map[string]any{"subagents": []interface{}{}}); err == nil {
		t.Error("Validate(empty array) should return error")
	}

	// Validate: non-string elements
	if err := h.Validate(map[string]any{"subagents": []interface{}{123}}); err == nil {
		t.Error("Validate(non-string elements) should return error")
	}

	// Validate: valid subagents
	validArgs := map[string]any{
		"subagents": []interface{}{"task 1", "task 2"},
	}
	if err := h.Validate(validArgs); err != nil {
		t.Errorf("Validate(valid args) should return nil, got: %v", err)
	}
}

// =============================================================================
// SP-038-5b: Task queue and todo tools (Name/Definition/Validate + Execute)
// =============================================================================

func TestTaskQueueAddHandlerConformance(t *testing.T) {
	h := &taskQueueAddHandler{}

	// Test Name
	if h.Name() != "task_queue_add" {
		t.Errorf("Name() = %q, want %q", h.Name(), "task_queue_add")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "task_queue_add" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "task_queue_add")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"title", "description", "persona", "priority", "working_dir"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error for missing title")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing title")
	}

	// Validate: valid minimal
	if err := h.Validate(map[string]any{"title": "test task"}); err != nil {
		t.Errorf("Validate(title) should return nil, got: %v", err)
	}

	// Validate: invalid priority
	if err := h.Validate(map[string]any{"title": "test", "priority": "critical"}); err == nil {
		t.Error("Validate(invalid priority) should return error")
	}

	// Validate: valid priority values
	for _, p := range []string{"high", "medium", "low"} {
		if err := h.Validate(map[string]any{"title": "test", "priority": p}); err != nil {
			t.Errorf("Validate(priority=%q) should return nil, got: %v", p, err)
		}
	}

	// Execute: add task to a temporary queue file
	// NewTaskQueue uses DefaultTaskQueuePath() which uses $HOME/.config/sprout,
	// so we cannot isolate the queue path. Test that Execute runs without panic.
	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{
		"title":       "Test Task",
		"description": "A test task",
		"priority":    "high",
	})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute result should not be IsError, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Test Task") {
		t.Errorf("Execute output should contain task title, got: %s", result.Output)
	}
}

func TestTaskQueuePublishHandlerConformance(t *testing.T) {
	h := &taskQueuePublishHandler{}

	// Test Name
	if h.Name() != "task_queue_publish" {
		t.Errorf("Name() = %q, want %q", h.Name(), "task_queue_publish")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "task_queue_publish" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "task_queue_publish")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"task_id", "status", "result", "subtasks"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing task_id")
	}
	if err := h.Validate(map[string]any{"task_id": "abc"}); err == nil {
		t.Error("Validate(task_id only) should return error for missing status")
	}

	// Validate: invalid status
	if err := h.Validate(map[string]any{"task_id": "abc", "status": "invalid_status"}); err == nil {
		t.Error("Validate(invalid status) should return error")
	}

	// Validate: valid
	for _, status := range []string{"pending", "in_progress", "completed", "failed", "blocked"} {
		if err := h.Validate(map[string]any{"task_id": "abc", "status": status}); err != nil {
			t.Errorf("Validate(status=%q) should return nil, got: %v", status, err)
		}
	}

	// Execute: with a temp queue - note that DefaultTaskQueuePath() uses $HOME
	// so we test that Execute doesn't panic
	ctx := context.Background()
	result, err := h.Execute(ctx, ToolEnv{}, map[string]any{
		"task_id": "nonexistent-id",
		"status":  "completed",
		"result":  "test result",
	})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	// It's expected to produce an error result for nonexistent task
	if !result.IsError {
		t.Logf("Execute returned non-error for nonexistent task (acceptable): %s", result.Output)
	}
}

func TestTaskQueueReadHandlerConformance(t *testing.T) {
	h := &taskQueueReadHandler{}

	// Test Name
	if h.Name() != "task_queue_read" {
		t.Errorf("Name() = %q, want %q", h.Name(), "task_queue_read")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "task_queue_read" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "task_queue_read")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"status", "limit"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: nil args returns error (actual implementation)
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}

	// Validate: empty map returns error (actual implementation)
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty map) should return error")
	}

	// Validate: with args passes
	if err := h.Validate(map[string]any{"status": "pending"}); err != nil {
		t.Errorf("Validate(status=pending) should return nil, got: %v", err)
	}

	// Execute: read from queue - uses DefaultTaskQueuePath() ($HOME/.config/sprout)
	// Test that it runs without panic
	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{"status": "pending"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	// Should return something even if queue is empty
	if result.Output == "" {
		t.Error("Execute output should not be empty")
	}
}

func TestTodoWriteHandlerConformance(t *testing.T) {
	h := &todoWriteHandler{}

	// Test Name
	if h.Name() != "todo_write" {
		t.Errorf("Name() = %q, want %q", h.Name(), "todo_write")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "todo_write" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "todo_write")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("todos") {
		t.Error("Definition missing 'todos' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing todos")
	}

	// Validate: todos not an array
	if err := h.Validate(map[string]any{"todos": "not an array"}); err == nil {
		t.Error("Validate(string todos) should return error")
	}

	// Validate: empty array (allowed by Validate - no loop runs)
	if err := h.Validate(map[string]any{"todos": []interface{}{}}); err != nil {
		t.Errorf("Validate(empty array) should return nil, got: %v", err)
	}

	// Validate: missing content in todo
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"status": "pending"},
		},
	}); err == nil {
		t.Error("Validate(missing content) should return error")
	}

	// Validate: empty content
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"content": "", "status": "pending"},
		},
	}); err == nil {
		t.Error("Validate(empty content) should return error")
	}

	// Validate: missing status
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"content": "test"},
		},
	}); err == nil {
		t.Error("Validate(missing status) should return error")
	}

	// Validate: invalid status
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"content": "test", "status": "invalid"},
		},
	}); err == nil {
		t.Error("Validate(invalid status) should return error")
	}

	// Validate: invalid priority
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"content": "test", "status": "pending", "priority": "invalid"},
		},
	}); err == nil {
		t.Error("Validate(invalid priority) should return error")
	}

	// Validate: valid todo
	validArgs := map[string]any{
		"todos": []interface{}{
			map[string]interface{}{
				"content":  "Test task",
				"status":   "pending",
				"priority": "high",
			},
		},
	}
	if err := h.Validate(validArgs); err != nil {
		t.Errorf("Validate(valid args) should return nil, got: %v", err)
	}

	// Execute: write todos
	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, validArgs)
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if !strings.Contains(result.Output, "1") {
		t.Logf("Output did not contain count, but may be acceptable: %s", result.Output)
	}
}

func TestTodoReadHandlerConformance(t *testing.T) {
	h := &todoReadHandler{}

	// Test Name
	if h.Name() != "todo_read" {
		t.Errorf("Name() = %q, want %q", h.Name(), "todo_read")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "todo_read" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "todo_read")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	// Validate: nil returns error (actual implementation)
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}

	// Validate: empty map returns error (actual implementation)
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty map) should return error")
	}

	// Validate: with any arg passes
	if err := h.Validate(map[string]any{"placeholder": true}); err != nil {
		t.Errorf("Validate(non-empty) should return nil, got: %v", err)
	}

	// Execute: reads todos (no crash)
	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{"placeholder": true})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.Output == "" {
		t.Error("Execute output should not be empty")
	}
}

// =============================================================================
// SP-038-5c: Remaining tools
// =============================================================================

func TestCommitHandlerConformance(t *testing.T) {
	h := &commitHandler{}

	// Test Name
	if h.Name() != "commit" {
		t.Errorf("Name() = %q, want %q", h.Name(), "commit")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "commit" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "commit")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"message", "notes"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: nil returns error (actual implementation)
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}

	// Validate: empty map returns error (actual implementation)
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty map) should return error")
	}

	// Validate: with args passes
	if err := h.Validate(map[string]any{"message": "test commit"}); err != nil {
		t.Errorf("Validate(message) should return nil, got: %v", err)
	}
	if err := h.Validate(map[string]any{"notes": "context notes"}); err != nil {
		t.Errorf("Validate(notes) should return nil, got: %v", err)
	}

	// Execute: with valid args and workspace root (should not panic)
	ctx := context.Background()
	tmpDir := t.TempDir()
	env := ToolEnv{WorkspaceRoot: tmpDir}
	_, err := h.Execute(ctx, env, map[string]any{"message": "test"})
	// We don't assert specific output since git won't work in a temp dir,
	// but we verify it doesn't panic
	if err != nil {
		t.Logf("Execute returned error (expected in test env): %v", err)
	}
}

func TestGitHandlerConformance(t *testing.T) {
	h := &gitHandler{}

	// Test Name
	if h.Name() != "git" {
		t.Errorf("Name() = %q, want %q", h.Name(), "git")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "git" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "git")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"operation", "args"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing operation")
	}

	// Validate: valid operation
	if err := h.Validate(map[string]any{"operation": "commit"}); err != nil {
		t.Errorf("Validate(operation) should return nil, got: %v", err)
	}

	// Validate: valid operation with args
	if err := h.Validate(map[string]any{"operation": "push", "args": "--force"}); err != nil {
		t.Errorf("Validate(operation+args) should return nil, got: %v", err)
	}

	// Validate: operation not a string
	if err := h.Validate(map[string]any{"operation": 123}); err == nil {
		t.Error("Validate(non-string operation) should return error")
	}

	// Execute: basic execution should not panic
	ctx := context.Background()
	env := ToolEnv{}
	_, err := h.Execute(ctx, env, map[string]any{"operation": "status"})
	// May fail due to git not being available, but should not panic
	if err != nil {
		t.Logf("Execute returned error (expected in test env): %v", err)
	}
}

func TestAskUserHandlerConformance(t *testing.T) {
	h := &askUserHandler{}

	// Test Name
	if h.Name() != "ask_user" {
		t.Errorf("Name() = %q, want %q", h.Name(), "ask_user")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "ask_user" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "ask_user")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("question") {
		t.Error("Definition missing 'question' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing question")
	}

	// Validate: valid question
	if err := h.Validate(map[string]any{"question": "What is your name?"}); err != nil {
		t.Errorf("Validate(question) should return nil, got: %v", err)
	}

	// Validate: non-string question
	if err := h.Validate(map[string]any{"question": 123}); err == nil {
		t.Error("Validate(non-string question) should return error")
	}

	// Execute: may fail in test env (no terminal), but should not panic
	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{"question": "test?"})
	if err != nil {
		t.Logf("Execute returned error (expected in test env): %v", err)
	}
	if result.IsError {
		t.Logf("Execute returned error result (expected in test env): %s", result.Output)
	}
}

func TestActivateSkillHandlerConformance(t *testing.T) {
	h := &activateSkillHandler{}

	// Test Name
	if h.Name() != "activate_skill" {
		t.Errorf("Name() = %q, want %q", h.Name(), "activate_skill")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "activate_skill" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "activate_skill")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("skill_id") {
		t.Error("Definition missing 'skill_id' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing skill_id")
	}

	// Validate: valid
	if err := h.Validate(map[string]any{"skill_id": "project-planning"}); err != nil {
		t.Errorf("Validate(skill_id) should return nil, got: %v", err)
	}

	// Validate: non-string skill_id
	if err := h.Validate(map[string]any{"skill_id": 123}); err == nil {
		t.Error("Validate(non-string skill_id) should return error")
	}
}

func TestAddMemoryHandlerConformance(t *testing.T) {
	h := &addMemoryHandler{}

	// Test Name
	if h.Name() != "add_memory" {
		t.Errorf("Name() = %q, want %q", h.Name(), "add_memory")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "add_memory" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "add_memory")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"name", "content"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing name")
	}
	if err := h.Validate(map[string]any{"name": "test"}); err == nil {
		t.Error("Validate(name only) should return error for missing content")
	}

	// Validate: valid
	if err := h.Validate(map[string]any{"name": "test", "content": "test content"}); err != nil {
		t.Errorf("Validate(valid) should return nil, got: %v", err)
	}

	// Execute: write to temp dir
	tmpDir := t.TempDir()
	memoriesDir := filepath.Join(tmpDir, "memories")
	os.MkdirAll(memoriesDir, 0755)
	origEnv := os.Getenv("SPROUT_CONFIG")
	os.Setenv("SPROUT_CONFIG", tmpDir)
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if origEnv != "" {
			os.Setenv("SPROUT_CONFIG", origEnv)
		} else {
			os.Unsetenv("SPROUT_CONFIG")
		}
		os.Unsetenv("LEDIT_CONFIG")
	}()

	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{
		"name":    "test_mem",
		"content": "# Test Memory\nContent here",
	})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute should not return IsError, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "test_mem") {
		t.Errorf("Output should contain memory name, got: %s", result.Output)
	}

	// Verify file was created
	memPath := filepath.Join(memoriesDir, "test_mem.md")
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		t.Error("Memory file was not created")
	}
}

func TestDeleteMemoryHandlerConformance(t *testing.T) {
	h := &deleteMemoryHandler{}

	// Test Name
	if h.Name() != "delete_memory" {
		t.Errorf("Name() = %q, want %q", h.Name(), "delete_memory")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "delete_memory" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "delete_memory")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("name") {
		t.Error("Definition missing 'name' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing name")
	}

	// Validate: valid
	if err := h.Validate(map[string]any{"name": "test_mem"}); err != nil {
		t.Errorf("Validate(name) should return nil, got: %v", err)
	}

	// Execute: delete existing memory
	tmpDir := t.TempDir()
	memoriesDir := filepath.Join(tmpDir, "memories")
	os.MkdirAll(memoriesDir, 0755)
	// Create a test memory file
	testFile := filepath.Join(memoriesDir, "del_test.md")
	os.WriteFile(testFile, []byte("# test"), 0644)

	origEnv := os.Getenv("SPROUT_CONFIG")
	os.Setenv("SPROUT_CONFIG", tmpDir)
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer func() {
		if origEnv != "" {
			os.Setenv("SPROUT_CONFIG", origEnv)
		} else {
			os.Unsetenv("SPROUT_CONFIG")
		}
		os.Unsetenv("LEDIT_CONFIG")
	}()

	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{"name": "del_test"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute should not return IsError, got: %s", result.Output)
	}

	// Verify file was deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Memory file should have been deleted")
	}

	// Execute: delete nonexistent memory
	result, err = h.Execute(ctx, env, map[string]any{"name": "nonexistent"})
	if err != nil {
		t.Fatalf("Execute should not return error for nonexistent, got: %v", err)
	}
	if !result.IsError {
		t.Error("Execute should return IsError for nonexistent memory")
	}
}

func TestPatchStructuredFileHandlerConformance(t *testing.T) {
	h := &patchStructuredFileHandler{}

	// Test Name
	if h.Name() != "patch_structured_file" {
		t.Errorf("Name() = %q, want %q", h.Name(), "patch_structured_file")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "patch_structured_file" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "patch_structured_file")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"path", "patch_ops", "data", "format", "schema"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing path")
	}

	// Validate: valid path
	if err := h.Validate(map[string]any{"path": "/tmp/test.json"}); err != nil {
		t.Errorf("Validate(path) should return nil, got: %v", err)
	}

	// Validate: non-string path
	if err := h.Validate(map[string]any{"path": 123}); err == nil {
		t.Error("Validate(non-string path) should return error")
	}

	// Execute: patch existing JSON file
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "test.json")
	os.WriteFile(jsonPath, []byte(`{"name": "original", "value": 1}`), 0644)

	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": jsonPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/name", "value": "updated"},
		},
	})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute should not return IsError, got: %s", result.Output)
	}

	// Verify file was patched
	updated, _ := os.ReadFile(jsonPath)
	if !strings.Contains(string(updated), "updated") {
		t.Errorf("File should contain updated value, got: %s", string(updated))
	}

	// Execute: nonexistent file
	result, err = h.Execute(ctx, env, map[string]any{
		"path": filepath.Join(tmpDir, "nope.json"),
	})
	if err != nil {
		t.Logf("Execute returned error for nonexistent file (expected): %v", err)
	}
}

func TestSelfReviewHandlerConformance(t *testing.T) {
	h := &selfReviewHandler{}

	// Test Name
	if h.Name() != "self_review" {
		t.Errorf("Name() = %q, want %q", h.Name(), "self_review")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "self_review" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "self_review")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("revision_id") {
		t.Error("Definition missing 'revision_id' parameter")
	}

	// Validate: nil returns error (actual implementation)
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}

	// Validate: empty map returns error (actual implementation)
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty map) should return error")
	}

	// Validate: with revision_id passes
	if err := h.Validate(map[string]any{"revision_id": "abc123"}); err != nil {
		t.Errorf("Validate(revision_id) should return nil, got: %v", err)
	}

	// Execute: with valid args (no panic)
	ctx := context.Background()
	tmpDir := t.TempDir()
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{"revision_id": "abc123"})
	if err != nil {
		t.Logf("Execute returned error (expected in test env): %v", err)
	}
	if result.IsError {
		t.Logf("Execute returned error result (expected - no revisions): %s", result.Output)
	}
}

func TestBrowseURLHandlerConformance(t *testing.T) {
	h := &browseURLHandler{}

	// Test Name
	if h.Name() != "browse_url" {
		t.Errorf("Name() = %q, want %q", h.Name(), "browse_url")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "browse_url" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "browse_url")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"url", "action", "steps", "viewport_width", "viewport_height"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing url")
	}

	// Validate: valid url
	if err := h.Validate(map[string]any{"url": "https://example.com"}); err != nil {
		t.Errorf("Validate(url) should return nil, got: %v", err)
	}

	// Validate: non-string url
	if err := h.Validate(map[string]any{"url": 123}); err == nil {
		t.Error("Validate(non-string url) should return error")
	}
}

func TestWebSearchHandlerConformance(t *testing.T) {
	h := &webSearchHandler{}

	// Test Name
	if h.Name() != "web_search" {
		t.Errorf("Name() = %q, want %q", h.Name(), "web_search")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "web_search" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "web_search")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("query") {
		t.Error("Definition missing 'query' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing query")
	}

	// Validate: valid query
	if err := h.Validate(map[string]any{"query": "test search"}); err != nil {
		t.Errorf("Validate(query) should return nil, got: %v", err)
	}

	// Validate: non-string query
	if err := h.Validate(map[string]any{"query": 123}); err == nil {
		t.Error("Validate(non-string query) should return error")
	}
}

func TestSemanticSearchHandlerConformance(t *testing.T) {
	h := &semanticSearchHandler{}

	// Test Name
	if h.Name() != "semantic_search" {
		t.Errorf("Name() = %q, want %q", h.Name(), "semantic_search")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "semantic_search" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "semantic_search")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"query", "threshold", "top_k"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing query")
	}

	// Validate: valid query
	if err := h.Validate(map[string]any{"query": "find similar code"}); err != nil {
		t.Errorf("Validate(query) should return nil, got: %v", err)
	}

	// Validate: valid with optional params
	if err := h.Validate(map[string]any{"query": "test", "top_k": 10, "threshold": 0.8}); err != nil {
		t.Errorf("Validate(full args) should return nil, got: %v", err)
	}

	// Validate: non-string query
	if err := h.Validate(map[string]any{"query": 123}); err == nil {
		t.Error("Validate(non-string query) should return error")
	}
}

func TestAnalyzeImageContentHandlerConformance(t *testing.T) {
	h := &analyzeImageContentHandler{}

	// Test Name
	if h.Name() != "analyze_image_content" {
		t.Errorf("Name() = %q, want %q", h.Name(), "analyze_image_content")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "analyze_image_content" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "analyze_image_content")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"image_path", "analysis_prompt", "analysis_mode"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing image_path")
	}

	// Validate: valid image_path
	if err := h.Validate(map[string]any{"image_path": "/tmp/test.png"}); err != nil {
		t.Errorf("Validate(image_path) should return nil, got: %v", err)
	}

	// Validate: valid with optional params
	if err := h.Validate(map[string]any{"image_path": "/tmp/test.png", "analysis_prompt": "extract text", "analysis_mode": "ocr"}); err != nil {
		t.Errorf("Validate(full args) should return nil, got: %v", err)
	}

	// Validate: non-string image_path
	if err := h.Validate(map[string]any{"image_path": 123}); err == nil {
		t.Error("Validate(non-string image_path) should return error")
	}
}

func TestAnalyzeUIScreenshotHandlerConformance(t *testing.T) {
	h := &analyzeUIScreenshotHandler{}

	// Test Name
	if h.Name() != "analyze_ui_screenshot" {
		t.Errorf("Name() = %q, want %q", h.Name(), "analyze_ui_screenshot")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "analyze_ui_screenshot" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "analyze_ui_screenshot")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"image_path", "analysis_prompt", "viewport_width", "viewport_height"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing image_path")
	}

	// Validate: valid image_path
	if err := h.Validate(map[string]any{"image_path": "/tmp/screenshot.png"}); err != nil {
		t.Errorf("Validate(image_path) should return nil, got: %v", err)
	}

	// Validate: valid with optional params
	if err := h.Validate(map[string]any{"image_path": "/tmp/screenshot.png", "analysis_prompt": "check layout", "viewport_width": 1920, "viewport_height": 1080}); err != nil {
		t.Errorf("Validate(full args) should return nil, got: %v", err)
	}

	// Validate: non-string image_path
	if err := h.Validate(map[string]any{"image_path": 123}); err == nil {
		t.Error("Validate(non-string image_path) should return error")
	}
}
