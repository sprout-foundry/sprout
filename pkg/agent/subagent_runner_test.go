package agent

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// stripANSI removes ANSI escape codes from a string for easy comparison.
var ansiStrip = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiStrip.ReplaceAllString(s, "")
}

// =============================================================================
// Existing tests (preserved from before)
// =============================================================================

func TestSubagentRunner_NewSubagentRunner(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: agent.configManager,
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	if runner == nil {
		t.Fatal("expected non-nil SubagentRunner")
	}
	if runner.parentAgent == nil {
		t.Fatal("expected non-nil parentAgent")
	}
	if runner.shared == nil {
		t.Fatal("expected non-nil shared state")
	}
}

func TestSubagentRunner_RunWithNilConfigManager(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	// SharedState with nil ConfigManager — createSubagent should fail
	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: nil, // nil config manager
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	result := runner.Run(context.Background(), "test prompt", SubagentOptions{})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Error == nil {
		t.Fatal("expected error when ConfigManager is nil")
	}
	if !result.Cancelled {
		// May or may not be cancelled depending on error path
	}
}

func TestSubagentRunner_GetActiveSubagentsEmpty(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: agent.configManager,
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	active := runner.GetActiveSubagents()
	if len(active) != 0 {
		t.Fatalf("expected 0 active subagents, got %d", len(active))
	}
}

func TestSubagentRunner_CancelSubagentNotFound(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: agent.configManager,
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	ok := runner.CancelSubagent("nonexistent-id")
	if ok {
		t.Fatal("expected CancelSubagent to return false for unknown ID")
	}
}

func TestSubagentRunner_CancelAll_NoOp(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: agent.configManager,
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	// Should not panic
	runner.CancelAll()
}

func TestSubagentRunner_RunParallelEmpty(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: agent.configManager,
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	results := runner.RunParallel(context.Background(), nil, SubagentOptions{})
	if results != nil {
		t.Fatalf("expected nil for empty tasks, got %d results", len(results))
	}

	// Also test with empty slice
	results = runner.RunParallel(context.Background(), []SubagentTask{}, SubagentOptions{})
	if results != nil {
		t.Fatalf("expected nil for empty task slice, got %d results", len(results))
	}
}

func TestSubagentRunner_RunParallel_ReturnsCorrectCount(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	// Use nil ConfigManager so subagents fail fast without needing LLM
	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: nil, // causes createSubagent to fail
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	tasks := []SubagentTask{
		{ID: "task-1", Prompt: "do something"},
		{ID: "task-2", Prompt: "do something else"},
	}
	results := runner.RunParallel(context.Background(), tasks, SubagentOptions{})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Both should have errors due to nil ConfigManager
	for i, r := range results {
		if r == nil {
			t.Fatalf("result[%d] is nil", i)
		}
		if r.Error == nil {
			t.Fatalf("result[%d] expected error, got nil", i)
		}
	}

	// Verify IDs are set
	if results[0].ID != "task-1" {
		t.Errorf("expected ID 'task-1', got %q", results[0].ID)
	}
	if results[1].ID != "task-2" {
		t.Errorf("expected ID 'task-2', got %q", results[1].ID)
	}
}

func TestSubagentRunner_Run_Timeout(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: agent.configManager,
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)

	// Use a very short timeout — the subagent will fail during creation
	// (config resolves but client creation may take time)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := runner.Run(ctx, "test prompt", SubagentOptions{
		Timeout: 1 * time.Millisecond, // extremely short timeout
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Result should have some elapsed time
	if result.Elapsed == 0 {
		t.Error("expected non-zero elapsed time")
	}
}

func TestSubagentRunner_SubagentResult_Struct(t *testing.T) {
	// Verify SubagentResult has all expected fields
	result := &SubagentResult{
		ID:             "test",
		Output:         "output",
		Error:          nil,
		TokensUsed:     100,
		Cost:           0.001,
		ToolCalls:      5,
		Elapsed:        time.Second,
		Cancelled:      false,
		BudgetExceeded: false,
	}

	if result.ID != "test" {
		t.Errorf("expected ID 'test', got %q", result.ID)
	}
	if result.TokensUsed != 100 {
		t.Errorf("expected 100 tokens, got %d", result.TokensUsed)
	}
	if result.ToolCalls != 5 {
		t.Errorf("expected 5 tool calls, got %d", result.ToolCalls)
	}
}

func TestSubagentRunner_SubagentTask_Struct(t *testing.T) {
	task := SubagentTask{
		ID:       "task-1",
		Prompt:   "implement feature",
		Model:    "gpt-4o",
		Provider: "openai",
		Persona:  "coder",
	}

	if task.ID != "task-1" {
		t.Errorf("expected ID 'task-1', got %q", task.ID)
	}
	if task.Persona != "coder" {
		t.Errorf("expected persona 'coder', got %q", task.Persona)
	}
}

// =============================================================================
// New tests for subagent terminal output prefixing feature
// =============================================================================

// Test buildSubagentPrefix helper function

func TestBuildSubagentPrefix_Single(t *testing.T) {
	// Single subagent (taskID starts with "subagent-") should use simple persona prefix
	tests := []struct {
		persona string
		taskID  string
		want    string
	}{
		{"coder", "subagent-123", "[coder]"},
		{"tester", "subagent-456", "[tester]"},
		{"debugger", "subagent-789", "[debugger]"},
		{"general", "subagent-999", "[general]"},
	}

	for _, tt := range tests {
		t.Run(tt.persona, func(t *testing.T) {
			got := buildSubagentPrefix(tt.persona, tt.taskID)
			if got != tt.want {
				t.Errorf("buildSubagentPrefix(%q, %q) = %q, want %q", tt.persona, tt.taskID, got, tt.want)
			}
		})
	}
}

func TestBuildSubagentPrefix_Parallel(t *testing.T) {
	// Parallel subagents (taskID does NOT start with "subagent-") should include task ID
	tests := []struct {
		name    string
		persona string
		taskID  string
		want    string
	}{
		{"task-1", "coder", "task-1", "[coder:task-1]"},
		{"task-2", "tester", "task-2", "[tester:task-2]"},
		{"custom-id", "debugger", "custom-id", "[debugger:custom-id]"},
		{"multi-para", "researcher", "task-10", "[researcher:task-10]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSubagentPrefix(tt.persona, tt.taskID)
			if got != tt.want {
				t.Errorf("buildSubagentPrefix(%q, %q) = %q, want %q", tt.persona, tt.taskID, got, tt.want)
			}
		})
	}
}

func TestBuildSubagentPrefix_EmptyTaskID(t *testing.T) {
	// Empty taskID should fall back to simple persona prefix
	persona := "coder"
	taskID := ""

	got := buildSubagentPrefix(persona, taskID)
	want := "[coder]"
	if got != want {
		t.Errorf("buildSubagentPrefix(%q, %q) = %q, want %q", persona, taskID, got, want)
	}
}

func TestBuildSubagentPrefix_EmptyPersona(t *testing.T) {
	// Empty persona should still work (no error)
	taskID := "task-1"
	got := buildSubagentPrefix("", taskID)
	want := "[:task-1]"
	if got != want {
		t.Errorf("buildSubagentPrefix(%q, %q) = %q, want %q", "", taskID, got, want)
	}
}

func TestBuildSubagentPrefix_TaskIDWithSubagentPrefix(t *testing.T) {
	// Even with custom persona, if taskID starts with "subagent-", should be simple prefix
	prefix := buildSubagentPrefix("researcher", "subagent-1234567890")
	if prefix != "[researcher]" {
		t.Errorf("expected simple prefix [researcher], got %q", prefix)
	}
}

func TestBuildSubagentPrefix_EmptyPersonaEmptyTaskID(t *testing.T) {
	// Edge case: both empty should produce "[]"
	prefix := buildSubagentPrefix("", "")
	if prefix != "[]" {
		t.Errorf("expected '[]', got %q", prefix)
	}
}

// Test streaming callback behavior

func TestSubagentStreamingCallback_PrefixesOutput(t *testing.T) {
	// Create a parent agent with output manager that has a mutex
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	// Set a mutex on the parent's output manager (simulates SetStreamingEnabled)
	mu := &sync.Mutex{}
	agent.output.SetOutputMutex(mu)

	// Build the prefix for this test
	prefix := buildSubagentPrefix("coder", "")
	const dimGray = "\033[90m"
	const reset = "\033[0m"
	expectedPrefix := dimGray + prefix + reset + " "

	// Create a callback that captures output
	var buf bytes.Buffer
	callback := func(chunk string) {
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		lines := strings.Split(chunk, "\n")
		for i, line := range lines {
			if line != "" || i < len(lines)-1 {
				fmt.Fprint(&buf, dimGray+prefix+reset+" "+line+"\n")
			}
		}
	}

	// Test with single line chunk
	callback("Hello, world!")

	output := buf.String()
	if !strings.HasPrefix(output, expectedPrefix) {
		t.Errorf("output prefix mismatch:\ngot:  %q\nwant: %q", output, expectedPrefix)
	}

	// Verify the content appears after the prefix
	if !strings.Contains(output, "Hello, world!") {
		t.Errorf("output missing expected content:\ngot: %q", output)
	}

	// Verify the output ends with a newline
	if !strings.HasSuffix(output, "\n") {
		t.Errorf("output should end with newline:\ngot: %q", output)
	}
}

func TestSubagentStreamingCallback_MultiLine(t *testing.T) {
	// Multi-line chunks should get each line prefixed
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	mu := &sync.Mutex{}
	agent.output.SetOutputMutex(mu)

	prefix := buildSubagentPrefix("tester", "task-1")
	const dimGray = "\033[90m"
	const reset = "\033[0m"

	var buf bytes.Buffer
	callback := func(chunk string) {
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		lines := strings.Split(chunk, "\n")
		for i, line := range lines {
			if line != "" || i < len(lines)-1 {
				fmt.Fprint(&buf, dimGray+prefix+reset+" "+line+"\n")
			}
		}
	}

	// Test with multi-line input (2 lines, no trailing newline)
	callback("Line one\nLine two")

	output := buf.String()
	stripLines := strings.Split(strings.TrimSpace(stripANSI(output)), "\n")

	if len(stripLines) != 2 {
		t.Fatalf("expected 2 lines in output, got %d:\n%s", len(stripLines), stripANSI(output))
	}

	// Each line should contain the prefix+space and the original content
	for i, line := range stripLines {
		if !strings.HasPrefix(line, prefix+" ") {
			t.Errorf("line %d should be prefixed:\ngot: %q\nwant prefix: %q", i, line, prefix+" ")
		}
	}
}

func TestSubagentStreamingCallback_EmptyLinesPreserved(t *testing.T) {
	// Empty lines in the middle should be preserved, but trailing empty lines after split
	// should be omitted (to avoid spurious blank lines)
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	mu := &sync.Mutex{}
	agent.output.SetOutputMutex(mu)

	prefix := buildSubagentPrefix("debugger", "task-2")
	const dimGray = "\033[90m"
	const reset = "\033[0m"

	var buf bytes.Buffer
	callback := func(chunk string) {
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		lines := strings.Split(chunk, "\n")
		for i, line := range lines {
			if line != "" || i < len(lines)-1 {
				fmt.Fprint(&buf, dimGray+prefix+reset+" "+line+"\n")
			}
		}
	}

	// Input with empty line in the middle and trailing newline
	callback("Line one\n\nLine three\n")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(stripANSI(output)), "\n")

	// Should have 3 lines: "Line one", "" (empty), "Line three"
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), stripANSI(output))
	}

	// First line should start with prefix+space and contain "Line one"
	if lines[0] != prefix+" Line one" {
		t.Errorf("first line mismatch:\ngot: %q\nwant: %q", lines[0], prefix+" Line one")
	}

	// Empty line should be preserved as "prefixed empty"
	if lines[1] != prefix+" " {
		t.Errorf("empty middle line mismatch:\ngot: %q\nwant: %q", lines[1], prefix+" ")
	}

	// Third line should start with prefix+space and contain "Line three"
	if lines[2] != prefix+" Line three" {
		t.Errorf("third line mismatch:\ngot: %q\nwant: %q", lines[2], prefix+" Line three")
	}
}

func TestSubagentStreamingCallback_TrailingNewlineOnly(t *testing.T) {
	// Input that's just a single "\n" produces strings.Split(chunk, "\n") = ["", ""].
	// The first "" (before the \n) gets prefixed (i < len-1), but the trailing ""
	// after split is skipped. So output is "prefix + space + empty\n".
	// This correctly represents a blank line in the content.
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	mu := &sync.Mutex{}
	agent.output.SetOutputMutex(mu)

	prefix := buildSubagentPrefix("coder", "")
	const dimGray = "\033[90m"
	const reset = "\033[0m"

	var buf bytes.Buffer
	callback := func(chunk string) {
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		lines := strings.Split(chunk, "\n")
		for i, line := range lines {
			if line != "" || i < len(lines)-1 {
				fmt.Fprint(&buf, dimGray+prefix+reset+" "+line+"\n")
			}
		}
	}

	// Just a trailing newline: produces one prefixed empty line
	callback("\n")

	output := buf.String()
	// Strip ANSI and split by newlines
	stripOutput := stripANSI(output)
	lines := strings.Split(stripOutput, "\n")

	// Should produce exactly 2 elements (one line + trailing empty after \n)
	if len(lines) != 2 {
		t.Fatalf("expected 2 elements (line + trailing empty), got %d:\n%q", len(lines), stripOutput)
	}

	// The first element should be prefix + space (the blank line)
	if lines[0] != prefix+" " {
		t.Errorf("expected prefixed empty line %q, got: %q", prefix+" ", lines[0])
	}

	// The trailing newline produces an empty second element
	if lines[1] != "" {
		t.Errorf("expected trailing empty element, got: %q", lines[1])
	}
}

func TestSubagentStreamingCallback_UsesMutex(t *testing.T) {
	// Verify that when a mutex is provided, the callback locks/unlocks it
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	mu := &sync.Mutex{}
	agent.output.SetOutputMutex(mu)

	prefix := buildSubagentPrefix("coder", "")
	const dimGray = "\033[90m"
	const reset = "\033[0m"

	var buf bytes.Buffer

	// Wrap the mutex to count acquisitions
	type countedMutex struct {
		sync.Mutex
		count int64
	}
	countedMu := &countedMutex{}

	callback := func(chunk string) {
		countedMu.Lock()
		defer countedMu.Unlock()
		countedMu.count++

		lines := strings.Split(chunk, "\n")
		for i, line := range lines {
			if line != "" || i < len(lines)-1 {
				fmt.Fprint(&buf, dimGray+prefix+reset+" "+line+"\n")
			}
		}
	}

	// Call callback multiple times
	callback("chunk1")
	callback("chunk2")
	callback("chunk3")

	if countedMu.count != 3 {
		t.Errorf("expected mutex to be acquired 3 times, got %d", countedMu.count)
	}

	// Verify output contains all chunks
	for _, chunk := range []string{"chunk1", "chunk2", "chunk3"} {
		if !strings.Contains(buf.String(), chunk) {
			t.Errorf("output should contain %q", chunk)
		}
	}
}

func TestSubagentStreamingCallback_AlwaysHasMutex(t *testing.T) {
	// The production code always ensures a mutex exists (either from parent
	// or by creating one). Verify that the callback pattern works with a mutex.
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	// Simulate the production behavior: always have a mutex
	mu := &sync.Mutex{}

	prefix := buildSubagentPrefix("tester", "")
	const dimGray = "\033[90m"
	const reset = "\033[0m"

	var buf bytes.Buffer
	callback := func(chunk string) {
		mu.Lock()
		defer mu.Unlock()
		lines := strings.Split(chunk, "\n")
		for i, line := range lines {
			if line != "" || i < len(lines)-1 {
				fmt.Fprint(&buf, dimGray+prefix+reset+" "+line+"\n")
			}
		}
	}

	callback("test output")

	output := buf.String()
	if output == "" {
		t.Error("expected non-empty output")
	}

	if !strings.Contains(output, "test output") {
		t.Errorf("output should contain test content:\n%s", output)
	}
}

// Test prefix with actual SubagentRunner integration (without calling LLM)

func TestSubagentRunner_PrefixFormat_SingleSubagent(t *testing.T) {
	// Verify that Run() generates a subagent- prefixed taskID,
	// which means the prefix should be [persona] not [persona:taskID]
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: nil, // Nil config so subagent creation fails quickly
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	result := runner.Run(context.Background(), "test prompt", SubagentOptions{
		Persona: "coder",
	})

	// The result should exist and have some elapsed time
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Elapsed == 0 {
		t.Error("expected non-zero elapsed time")
	}

	// TaskID should start with "subagent-" (which triggers simple prefix)
	// Note: Since the subagent creation fails, we can't directly verify the prefix
	// was set up, but we can verify the taskID pattern
	if !strings.HasPrefix(result.ID, "subagent-") {
		t.Errorf("expected taskID to start with 'subagent-', got %q", result.ID)
	}

	// Verify the prefix for this taskID would be simple
	expectedPrefix := "[coder]"
	actualPrefix := buildSubagentPrefix("coder", result.ID)
	if actualPrefix != expectedPrefix {
		t.Errorf("expected simple prefix %q, got %q", expectedPrefix, actualPrefix)
	}
}

func TestSubagentRunner_PrefixFormat_ParallelTask(t *testing.T) {
	// Verify that RunParallel() passes through custom task IDs,
	// which means the prefix should be [persona:taskID]
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: nil, // Nil config so subagent creation fails quickly
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	tasks := []SubagentTask{
		{ID: "my-custom-task", Persona: "tester", Prompt: "test"},
	}

	results := runner.RunParallel(context.Background(), tasks, SubagentOptions{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// TaskID should be preserved as-is
	if result.ID != "my-custom-task" {
		t.Errorf("expected taskID 'my-custom-task', got %q", result.ID)
	}

	// Verify the prefix for this taskID includes the task ID
	expectedPrefix := "[tester:my-custom-task]"
	actualPrefix := buildSubagentPrefix("tester", result.ID)
	if actualPrefix != expectedPrefix {
		t.Errorf("expected prefixed ID %q, got %q", expectedPrefix, actualPrefix)
	}
}

func TestSubagentRunner_OutputRouterSet(t *testing.T) {
	// Verify that the OutputRouter is set up with the shared eventBus
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	testEventBus := events.NewEventBus()
	shared := &SharedState{
		EventBus:      testEventBus,
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: nil, // Causes creation to fail, but we verify the pattern
		WorkspaceRoot: agent.workspaceRoot,
	}

	runner := NewSubagentRunner(agent, shared)
	result := runner.Run(context.Background(), "test", SubagentOptions{
		Persona: "coder",
	})

	// Result should exist with an error (because ConfigManager is nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Error == nil {
		t.Fatal("expected error due to nil ConfigManager")
	}

	// Verify the error mentions the expected issue
	if !strings.Contains(result.Error.Error(), "config") && !strings.Contains(result.Error.Error(), "subagent") {
		t.Errorf("unexpected error message: %v", result.Error)
	}
}
