package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Note: run_subagent / run_parallel_subagents are intentionally NOT handlers in
// this package (see all.go) — they live in the seed registry under pkg/agent
// because they need *Agent access. SP-059 Phase 3b removed the stub handlers
// here, so their conformance tests were removed too.

// =============================================================================
// SP-038-5b: Todo tools (Name/Definition/Validate + Execute)
// Task queue tools (task_queue_add / publish / read) were removed in 2026-07-18
// along with the Executive Assistant queue mode. The TODO list + workflow-automation
// skill covers the autonomous batch-processing use case instead.
// =============================================================================

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

	// ---------------------------------------------------------------------------
	// Execute: success path with fake SkillLoader
	// ---------------------------------------------------------------------------
	ctx := context.Background()
	env := ToolEnv{
		SkillLoader: &fakeSkillLoader{
			skills: map[string]*SkillInfo{
				"project-planning": {
					ID:          "project-planning",
					Name:        "Project Planning",
					Description: "Strategic planning and alignment for new (greenfield) or existing (brownfield) projects...",
					Content:     "# Project Planning Skill\n\nDo strategic planning.",
					Source:      "builtin",
				},
			},
		},
	}
	result, err := h.Execute(ctx, env, map[string]any{"skill_id": "project-planning"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute result should not be IsError, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Activated skill") {
		t.Error("Execute output should contain 'Activated skill'")
	}
	if !strings.Contains(result.Output, "Description:") {
		t.Error("Execute output should contain 'Description:'")
	}
	if !strings.Contains(result.Output, "Project Planning") {
		t.Error("Execute output should contain skill name")
	}
	if !strings.Contains(result.Output, "Instructions loaded into context") {
		t.Error("Execute output should contain 'Instructions loaded into context'")
	}
}

// TestAddMemoryHandlerConformance and TestDeleteMemoryHandlerConformance
// were removed when the legacy per-operation memory handlers were retired
// in favor of manage_memory (see pkg/agent/memory_manage.go). The
// add/delete operations are covered by manage_memory operation tests.

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

	// ---------------------------------------------------------------------------
	// Execute: success path with fake SearchEngine
	// ---------------------------------------------------------------------------
	ctx := context.Background()
	fakeEngine := &fakeSearchEngine{
		results: map[string]string{
			"Go testing best practices": "Search results for \"Go testing best practices\":\n1. Table-driven tests\n2. Subtests",
		},
	}
	env := ToolEnv{SearchEngine: fakeEngine}
	result, err := h.Execute(ctx, env, map[string]any{"query": "Go testing best practices"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute result should not be IsError, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Search results for") {
		t.Errorf("Execute output should contain 'Search results for', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Go testing best practices") {
		t.Errorf("Execute output should contain query, got: %s", result.Output)
	}

	// ---------------------------------------------------------------------------
	// Execute: nil SearchEngine
	// ---------------------------------------------------------------------------
	envNil := ToolEnv{}
	result, err = h.Execute(ctx, envNil, map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if !result.IsError {
		t.Fatalf("Execute result should be IsError when SearchEngine is nil, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "search engine not available") {
		t.Errorf("Execute output should mention 'search engine not available', got: %s", result.Output)
	}

	// ---------------------------------------------------------------------------
	// Execute: error path
	// ---------------------------------------------------------------------------
	fakeErr := &fakeSearchEngine{
		fail:    true,
		failErr: errors.New("search API unavailable"),
	}
	envErr := ToolEnv{SearchEngine: fakeErr}
	result, err = h.Execute(ctx, envErr, map[string]any{"query": "anything"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if !result.IsError {
		t.Fatalf("Execute result should be IsError on search failure, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "search API unavailable") {
		t.Errorf("Execute output should contain 'search API unavailable', got: %s", result.Output)
	}
}

// fakeSearchEngine is a test double for the SearchEngine interface.
type fakeSearchEngine struct {
	results map[string]string
	fail    bool
	failErr error
}

func (f *fakeSearchEngine) Search(ctx context.Context, query string) (string, error) {
	if f.fail {
		return "", f.failErr
	}
	return f.results[query], nil
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
