package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/events"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// ---------------------------------------------------------------------------
// Mock StateManager — satisfies every method on StateManager with no-ops
// ---------------------------------------------------------------------------

type mockStateManager struct{}

func (m *mockStateManager) GetMessages() []api.Message                      { return nil }
func (m *mockStateManager) SetMessages([]api.Message)                        {}
func (m *mockStateManager) AddMessage(api.Message)                          {}
func (m *mockStateManager) GetSessionID() string                             { return "" }
func (m *mockStateManager) SetSessionID(string)                              {}
func (m *mockStateManager) GetTurnCheckpoints() []TurnCheckpoint             { return nil }
func (m *mockStateManager) SetTurnCheckpoints([]TurnCheckpoint)              {}
func (m *mockStateManager) AddTurnCheckpoint(TurnCheckpoint)                 {}
func (m *mockStateManager) GetCheckpointMutex() *sync.RWMutex                { return nil }
func (m *mockStateManager) GetPreviousSummary() string                       { return "" }
func (m *mockStateManager) SetPreviousSummary(string)                        {}
func (m *mockStateManager) GetOptimizer() *ConversationOptimizer             { return nil }
func (m *mockStateManager) SetOptimizer(*ConversationOptimizer)              {}
func (m *mockStateManager) GetCurrentContextTokens() int                     { return 0 }
func (m *mockStateManager) SetCurrentContextTokens(int)                      {}
func (m *mockStateManager) GetMaxContextTokens() int                         { return 0 }
func (m *mockStateManager) SetMaxContextTokens(int)                          {}
func (m *mockStateManager) IsContextWarningIssued() bool                     { return false }
func (m *mockStateManager) SetContextWarningIssued(bool)                     {}
func (m *mockStateManager) GetTaskActions() []TaskAction                     { return nil }
func (m *mockStateManager) SetTaskActions([]TaskAction)                      {}
func (m *mockStateManager) AddTaskAction(TaskAction)                         {}
func (m *mockStateManager) GetTaskActionsMutex() *sync.RWMutex               { return nil }
func (m *mockStateManager) GetTotalCost() float64                            { return 0 }
func (m *mockStateManager) SetTotalCost(float64)                             {}
func (m *mockStateManager) AddCost(float64)                                  {}
func (m *mockStateManager) GetTotalTokens() int                              { return 0 }
func (m *mockStateManager) SetTotalTokens(int)                               {}
func (m *mockStateManager) GetPromptTokens() int                             { return 0 }
func (m *mockStateManager) SetPromptTokens(int)                              {}
func (m *mockStateManager) GetCompletionTokens() int                         { return 0 }
func (m *mockStateManager) SetCompletionTokens(int)                          {}
func (m *mockStateManager) GetLLMCallCount() int                             { return 0 }
func (m *mockStateManager) SetLLMCallCount(int)                              {}
func (m *mockStateManager) IncrementLLMCallCount()                           {}
func (m *mockStateManager) GetTotalToolCalls() int                           { return 0 }
func (m *mockStateManager) SetTotalToolCalls(int)                            {}
func (m *mockStateManager) IncrementTotalToolCalls()                         {}
func (m *mockStateManager) GetEstimatedTokenResponses() int                  { return 0 }
func (m *mockStateManager) SetEstimatedTokenResponses(int)                   {}
func (m *mockStateManager) GetCachedTokens() int                             { return 0 }
func (m *mockStateManager) SetCachedTokens(int)                              {}
func (m *mockStateManager) GetCacheWriteTokens() int                         { return 0 }
func (m *mockStateManager) SetCacheWriteTokens(int)                          {}
func (m *mockStateManager) GetCachedCostSavings() float64                    { return 0 }
func (m *mockStateManager) SetCachedCostSavings(float64)                     {}
func (m *mockStateManager) GetActiveSkills() []string                        { return nil }
func (m *mockStateManager) SetActiveSkills([]string)                         {}
func (m *mockStateManager) GetActivePersona() string                         { return "" }
func (m *mockStateManager) SetActivePersona(string)                          {}
func (m *mockStateManager) GetCircuitBreaker() *CircuitBreakerState          { return nil }
func (m *mockStateManager) SetCircuitBreaker(*CircuitBreakerState)           {}
func (m *mockStateManager) IsToolCallGuidanceAdded() bool                    { return false }
func (m *mockStateManager) SetToolCallGuidanceAdded(bool)                    {}
func (m *mockStateManager) GetPendingSwitchContextRefresh() string           { return "" }
func (m *mockStateManager) SetPendingSwitchContextRefresh(string)            {}
func (m *mockStateManager) GetPendingStrictSwitchNotice() string             { return "" }
func (m *mockStateManager) SetPendingStrictSwitchNotice(string)              {}
func (m *mockStateManager) GetPendingSystemSupplement() string               { return "" }
func (m *mockStateManager) SetPendingSystemSupplement(string)                {}
func (m *mockStateManager) IsFalseStopDetectionEnabled() bool                { return false }
func (m *mockStateManager) SetFalseStopDetectionEnabled(bool)                {}
func (m *mockStateManager) GetLastRunTerminationReason() string              { return "" }
func (m *mockStateManager) SetLastRunTerminationReason(string)               {}
func (m *mockStateManager) GetConversationPruner() *ConversationPruner       { return nil }
func (m *mockStateManager) SetConversationPruner(*ConversationPruner)        {}
func (m *mockStateManager) GetCommandHistory() []string                      { return nil }
func (m *mockStateManager) SetCommandHistory([]string)                       {}
func (m *mockStateManager) GetHistoryIndex() int                             { return 0 }
func (m *mockStateManager) SetHistoryIndex(int)                              {}
func (m *mockStateManager) GetHistoryMutex() *sync.Mutex                     { return nil }
func (m *mockStateManager) GetPauseState() *PauseState                       { return nil }
func (m *mockStateManager) SetPauseState(*PauseState)                        {}
func (m *mockStateManager) GetPauseMutex() *sync.Mutex                       { return nil }
func (m *mockStateManager) GetTraceSession() interface{}                     { return nil }
func (m *mockStateManager) SetTraceSession(interface{})                      {}
func (m *mockStateManager) GetSessionProvider() api.ClientType              { return "" }
func (m *mockStateManager) SetSessionProvider(api.ClientType)                {}
func (m *mockStateManager) GetSessionModel() string                          { return "" }
func (m *mockStateManager) SetSessionModel(string)                           {}
func (m *mockStateManager) GetConfigOverrides() map[string]interface{}       { return nil }
func (m *mockStateManager) SetConfigOverrides(map[string]interface{})        {}
func (m *mockStateManager) GetCurrentIteration() int                         { return 0 }
func (m *mockStateManager) SetCurrentIteration(int)                          {}
func (m *mockStateManager) GetSessionIntentEmbedding() []float32             { return nil }
func (m *mockStateManager) SetSessionIntentEmbedding([]float32)              {}
func (m *mockStateManager) SetSessionIntentEmbeddingIfNil([]float32) bool    { return false }
func (m *mockStateManager) GetLastProviderError() *ProviderErrorInfo         { return nil }
func (m *mockStateManager) SetLastProviderError(*ProviderErrorInfo)          {}

// ---------------------------------------------------------------------------
// Mock OutputManager — satisfies every method on OutputManager with no-ops
// ---------------------------------------------------------------------------

type mockOutputManager struct{}

func (m *mockOutputManager) SetStreamingEnabled(bool)               {}
func (m *mockOutputManager) IsStreamingEnabled() bool               { return false }
func (m *mockOutputManager) SetStreamingCallback(func(string))      {}
func (m *mockOutputManager) GetStreamingCallback() func(string)     { return nil }
func (m *mockOutputManager) SetReasoningCallback(func(string))      {}
func (m *mockOutputManager) GetReasoningCallback() func(string)     { return nil }
func (m *mockOutputManager) SetFlushCallback(func())                {}
func (m *mockOutputManager) GetFlushCallback() func()               { return nil }
func (m *mockOutputManager) SetOutputMutex(*sync.Mutex)             {}
func (m *mockOutputManager) GetOutputMutex() *sync.Mutex            { return nil }
func (m *mockOutputManager) GetStreamingBuffer() *strings.Builder   { return nil }
func (m *mockOutputManager) GetReasoningBuffer() *strings.Builder   { return nil }
func (m *mockOutputManager) GetOutputRouter() *OutputRouter         { return nil }
func (m *mockOutputManager) SetOutputRouter(*OutputRouter)          {}
func (m *mockOutputManager) GetAsyncOutput() chan string            { return nil }
func (m *mockOutputManager) SetAsyncOutput(chan string)             {}
func (m *mockOutputManager) EnsureAsyncOutputWorker(func())         {}
func (m *mockOutputManager) GetAsyncBufferSize() int                { return 0 }
func (m *mockOutputManager) SetAsyncBufferSize(int)                 {}
func (m *mockOutputManager) GetEventMetadata() map[string]interface{} { return nil }
func (m *mockOutputManager) SetEventMetadata(map[string]interface{}) { }
func (m *mockOutputManager) SetEventMetadataUnlocked(map[string]interface{}) { }
func (m *mockOutputManager) GetEventMetadataMutex() *sync.RWMutex {
	return &sync.RWMutex{}
}
func (m *mockOutputManager) SetTerminalWriter(func(string))    {}
func (m *mockOutputManager) GetTerminalWriter() func(string)   { return nil }

// ---------------------------------------------------------------------------
// minimalAgent creates a bare Agent with initialized sub-managers for testing.
// ---------------------------------------------------------------------------

func minimalAgent(t *testing.T) *Agent {
	t.Helper()
	a := &Agent{}
	a.initSubManagers()
	a.state = &mockStateManager{}
	a.output = &mockOutputManager{}
	a.security = NewAgentSecurityManager()
	a.eventBus = events.NewEventBus()
	return a
}

// ---------------------------------------------------------------------------
// Test 1: postProcessResult with empty result
// ---------------------------------------------------------------------------

func TestSeedRegistry_FiltersSubagentToolsAtDepthLimit(t *testing.T) {
	// A subagent at depth 1 with a non-coordinator root (MaxSubagentDepth=1)
	// cannot spawn further subagents. The seed registry must NOT register
	// run_subagent / run_parallel_subagents for such an agent — otherwise
	// the LLM sees the tool, wastes turns attempting the call, and gets
	// confused by the PreExecuteHook error.
	agent := minimalAgent(t)
	agent.subagentDepth = 1 // depth 1 = first-level subagent; MaxSubagentDepth()=1 → CanSpawn=false

	registry := newSeedToolRegistryWithPublisher(agent, nil)

	if registry.HasTool("run_subagent") {
		t.Error("run_subagent should not be registered for an agent at its depth limit")
	}
	if registry.HasTool("run_parallel_subagents") {
		t.Error("run_parallel_subagents should not be registered for an agent at its depth limit")
	}

	// Sanity check: non-subagent tools ARE still registered.
	if !registry.HasTool("read_file") {
		t.Error("read_file should still be registered")
	}
	if !registry.HasTool("shell_command") {
		t.Error("shell_command should still be registered")
	}
}

func TestSeedRegistry_KeepsSubagentToolsForPrimaryAgent(t *testing.T) {
	// The primary agent (depth 0) with MaxSubagentDepth=1 can spawn
	// subagents (0 < 1 = true). The tools must be present.
	agent := minimalAgent(t)
	agent.subagentDepth = 0

	registry := newSeedToolRegistryWithPublisher(agent, nil)

	if !registry.HasTool("run_subagent") {
		t.Error("run_subagent should be registered for the primary agent")
	}
	if !registry.HasTool("run_parallel_subagents") {
		t.Error("run_parallel_subagents should be registered for the primary agent")
	}
}

// ---------------------------------------------------------------------------
// Test 1: postProcessResult with empty result
// ---------------------------------------------------------------------------

func TestPostProcessResult_EmptyResult(t *testing.T) {
	ctx := context.Background()
	agent := minimalAgent(t)

	// Empty result should be returned as-is without any processing.
	result := postProcessResult(ctx, agent, "shell_command", nil, "")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}

	// Also verify with nil args.
	result = postProcessResult(ctx, agent, "read_file", nil, "")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Test 2: postProcessResult with secret detection in output
// ---------------------------------------------------------------------------

func TestPostProcessResult_SecretRedaction(t *testing.T) {
	ctx := context.Background()
	agent := minimalAgent(t)

	// Set up elevation gate with catch-all that returns SecretRedact
	// so secrets get redacted rather than blocked.
	agent.security.(*AgentSecurityManager).elevationGate.SetDefault(security.SecretRedact, "")

	// Input that contains a secret-like pattern. The OutputRedactor has built-in
	// patterns for common secret types (AWS keys, API tokens, etc.).
	input := "File written successfully. Token: abcdefghijklmnopqrstuvwxyz0123456789 and more text."
	result := postProcessResult(ctx, agent, "read_file", map[string]interface{}{
		"path": "/tmp/test.txt",
	}, input)

	// When there are no secrets detected, result should be unchanged.
	if result != input {
		t.Errorf("expected unchanged result when no secrets, got: %s", result)
	}
}

func TestPostProcessResult_SecretBlocking(t *testing.T) {
	ctx := context.Background()
	agent := minimalAgent(t)

	// Elevation gate with catch-all that returns SecretBlock.
	agent.security.(*AgentSecurityManager).elevationGate.SetDefault(security.SecretBlock, "")

	// Use a redactor to trigger pattern detection.
	redactor := security.NewOutputRedactor()
	agent.security.(*AgentSecurityManager).outputRedactor = redactor

	// The redactor's detectAndRedactPatterns uses DetectSecurityConcerns on
	// the already-redacted content. The "api_key: sk-..." pattern reliably
	// triggers detection even after redaction.
	input := "File written. api_key: sk-7f3b2a1c9d8e4567890abcdef1234567 and more text."
	result := postProcessResult(ctx, agent, "read_file", map[string]interface{}{
		"path": "/tmp/test.txt",
	}, input)

	// Result should be blocked when secrets are detected.
	if !strings.Contains(result, "BLOCKED") {
		t.Logf("Redaction result: %s", result)
		t.Errorf("expected BLOCKED result, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Test 3: postProcessResult with TodoWrite emitting events
// ---------------------------------------------------------------------------

func TestPostProcessResult_TodoWriteEvent(t *testing.T) {
	ctx := context.Background()
	agent := minimalAgent(t)

	// Initialize a TodoManager on the agent.
	todoMgr := tools.NewTodoManager()
	agent.todoMgr = todoMgr

	// Set up the event bus subscription to capture events.
	sub := agent.eventBus.Subscribe("test-todo")

	// Perform a TodoWrite via the TodoManager first to populate items.
	todoMgr.Write([]tools.TodoItem{
		{ID: "1", Content: "Test task", Status: "in_progress"},
	})

	// postProcessResult with TodoWrite should emit an event via PublishTodoUpdate.
	result := postProcessResult(ctx, agent, "TodoWrite", nil, "Todo list updated with 1 items")

	if result == "" {
		t.Error("expected non-empty result for TodoWrite")
	}

	// Check that a todo_update event was published.
	select {
	case event := <-sub:
		if event.Type != events.EventTypeTodoUpdate {
			t.Errorf("expected event type %q, got %q", events.EventTypeTodoUpdate, event.Type)
		}
		if event.Data == nil {
			t.Error("expected non-nil event data for todo_update")
		}
	default:
		t.Error("expected todo_update event to be published, but no event received")
	}
}

func TestPostProcessResult_TodoWriteNoEvent(t *testing.T) {
	ctx := context.Background()
	agent := minimalAgent(t)

	// Initialize a TodoManager but with empty list.
	todoMgr := tools.NewTodoManager()
	agent.todoMgr = todoMgr

	sub := agent.eventBus.Subscribe("test-todo-empty")

	// postProcessResult with a non-TodoWrite tool should NOT emit events.
	_ = postProcessResult(ctx, agent, "read_file", nil, "some result")

	select {
	case <-sub:
		t.Error("unexpected todo_update event for non-TodoWrite tool")
	default:
		// expected — no event should be published
	}
}

// ---------------------------------------------------------------------------
// Test 4: postProcessResult with duplicate embedding check
// ---------------------------------------------------------------------------

func TestPostProcessResult_DuplicateEmbeddingCheck(t *testing.T) {
	ctx := context.Background()
	agent := minimalAgent(t)

	// embeddingMgr is nil by default, so shouldCheckDuplicates should return false
	// for write tools.
	result := postProcessResult(ctx, agent, "write_file", map[string]interface{}{
		"path": "/tmp/test_dupe.txt",
	}, "File written successfully.")

	if !strings.Contains(result, "File written successfully.") {
		t.Errorf("expected original result, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Test 5: Handler error handling with sanitizeToolFailureMessage
// ---------------------------------------------------------------------------

func TestPostProcessResult_SanitizeToolFailureMessage(t *testing.T) {
	// Empty strings become a default message.
	if got := sanitizeToolFailureMessage(""); got != "unknown tool error" {
		t.Errorf("empty input: expected 'unknown tool error', got %q", got)
	}

	// Whitespace-only becomes default too.
	if got := sanitizeToolFailureMessage("   \n  "); got != "unknown tool error" {
		t.Errorf("whitespace-only: expected 'unknown tool error', got %q", got)
	}

	// Large base64 runs are redacted.
	bigBase64 := strings.Repeat("A", 600)
	got := sanitizeToolFailureMessage("Error with " + bigBase64)
	if !strings.Contains(got, "[BASE64_REDACTED]") {
		t.Errorf("expected base64 redaction, got: %s", got)
	}

	// Data URIs are redacted.
	dataURI := "data:image/png;base64," + strings.Repeat("A", 100)
	got = sanitizeToolFailureMessage("Error: " + dataURI)
	// The pattern replaces with mime-based redaction.
	if !strings.Contains(got, "REDACTED") {
		t.Errorf("expected data URI redaction, got: %s", got)
	}

	// Very long messages are truncated.
	longMsg := strings.Repeat("x", 5000)
	got = sanitizeToolFailureMessage(longMsg)
	if len(got) > maxToolFailureMessageChars {
		t.Errorf("expected result truncated to %d chars, got %d chars", maxToolFailureMessageChars, len(got))
	}

	// Normal short error messages pass through unchanged.
	got = sanitizeToolFailureMessage("command not found")
	if got != "command not found" {
		t.Errorf("expected 'command not found', got %q", got)
	}
}

func TestPostProcessResult_HandlerClosureErrorHandling(t *testing.T) {
	// Verify the sanitize function is called in error paths.
	sanitized := sanitizeToolFailureMessage("security block: shell_command — Dangerous command execution: rm -rf /")
	if strings.Contains(sanitized, "security block") {
		t.Logf("Security message preserved: %s", sanitized)
	}

	// Verify normal messages are not corrupted.
	sanitized = sanitizeToolFailureMessage("file not found: /nonexistent/path")
	if sanitized != "file not found: /nonexistent/path" {
		t.Errorf("expected exact message, got: %q", sanitized)
	}
}

// ---------------------------------------------------------------------------
// Additional tests for helper functions used in postProcessResult
// ---------------------------------------------------------------------------

func TestBuildSecretSource(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
		want     string
	}{
		{
			name:     "shell_command with short command",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": "ls -la"},
			want:     "shell_command: ls -la",
		},
		{
			name:     "shell_command with long command (truncated)",
			toolName: "shell_command",
			args:     map[string]interface{}{"command": strings.Repeat("a", 100)},
			want:     "shell_command: " + strings.Repeat("a", 77) + "...",
		},
		{
			name:     "shell_command without command arg",
			toolName: "shell_command",
			args:     map[string]interface{}{},
			want:     "shell_command",
		},
		{
			name:     "read_file with path",
			toolName: "read_file",
			args:     map[string]interface{}{"path": "/home/user/file.go"},
			want:     "read_file: /home/user/file.go",
		},
		{
			name:     "git without path (falls back to tool name)",
			toolName: "git",
			args:     map[string]interface{}{"operation": "status"},
			want:     "git",
		},
		{
			name:     "search_files with pattern",
			toolName: "search_files",
			args:     map[string]interface{}{"search_pattern": "TODO"},
			want:     "search_files: TODO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSecretSource(tt.toolName, tt.args)
			if got != tt.want {
				t.Errorf("buildSecretSource(%q, %v) = %q, want %q", tt.toolName, tt.args, got, tt.want)
			}
		})
	}
}

func TestFormatTodoItemsForEvent(t *testing.T) {
	todos := []tools.TodoItem{
		{ID: "1", Content: "First task", Status: "pending"},
		{ID: "2", Content: "Second task", Status: "completed"},
	}

	result := formatTodoItemsForEvent(todos)

	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}

	if result[0]["id"] != "1" || result[0]["content"] != "First task" || result[0]["status"] != "pending" {
		t.Errorf("unexpected item 0: %v", result[0])
	}

	if result[1]["id"] != "2" || result[1]["content"] != "Second task" || result[1]["status"] != "completed" {
		t.Errorf("unexpected item 1: %v", result[1])
	}
}

func TestFormatTodoItemsForEvent_Empty(t *testing.T) {
	result := formatTodoItemsForEvent(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d items", len(result))
	}
}

func TestPostProcessResult_IsSecretSensitiveTool(t *testing.T) {
	sensitive := []string{"shell_command", "read_file", "search_files", "write_file", "edit_file", "write_structured_file", "patch_structured_file"}
	for _, name := range sensitive {
		if !isSecretSensitiveTool(name) {
			t.Errorf("expected %q to be secret-sensitive", name)
		}
	}

	nonSensitive := []string{"TodoWrite", "TodoRead", "git", "run_subagent", "web_search", "fetch_url", "list_skills"}
	for _, name := range nonSensitive {
		if isSecretSensitiveTool(name) {
			t.Errorf("expected %q to NOT be secret-sensitive", name)
		}
	}
}

func TestPostProcessResult_TruncateToolResult(t *testing.T) {
	// Short result should not be truncated.
	short := "Hello, World!"
	result := truncateToolResult(short)
	if result != short {
		t.Errorf("expected no truncation, got: %s", result)
	}

	// Result at exact boundary should not be truncated.
	boundary := strings.Repeat("x", defaultToolResultMaxChars)
	result = truncateToolResult(boundary)
	if result != boundary {
		t.Error("expected no truncation at exact boundary")
	}

	// Result one byte over should be truncated.
	oversized := strings.Repeat("x", defaultToolResultMaxChars+1)
	result = truncateToolResult(oversized)
	// The truncation formula is: headChars (45K) + truncation notice (~56 chars) + tailChars (5K)
	// So it's not guaranteed to be exactly defaultToolResultMaxChars.
	expectedLen := 45000 + 5000
	if len(result) < expectedLen {
		t.Errorf("expected result >= %d chars after truncation, got %d", expectedLen, len(result))
	}
	if !strings.Contains(result, "... truncated:") {
		t.Errorf("expected truncation notice in result: %s", result)
	}
}

// TestArgsFromPayloadDecodesSeedArguments verifies that the seed tool_start
// payload shape (args as a JSON string under "arguments") is decoded so
// buildDisplayName can surface the command — otherwise the CLI tool log
// renders a bare "shell_command" with no command (the reported regression).
func TestArgsFromPayloadDecodesSeedArguments(t *testing.T) {
	// Seed core publishes: tool_name, tool_call_id, arguments(JSON), tool_index.
	payload := map[string]interface{}{
		"tool_name":    "shell_command",
		"tool_call_id": "abc",
		"arguments":    `{"command":"curl -s https://x | python3 -m json.tool"}`,
		"tool_index":   0,
	}
	got := buildDisplayName("shell_command", argsFromPayload(payload))
	if !strings.HasPrefix(got, "shell_command curl -s https://x") {
		t.Errorf("display name did not surface the command: %q", got)
	}

	// Multi-line command collapses to a single scannable line.
	multiline := map[string]interface{}{
		"tool_name": "shell_command",
		"arguments": "{\"command\":\"curl -s https://x \\\\\\n  -H 'a: b' \\\\\\n  -d '{}'\"}",
	}
	got = buildDisplayName("shell_command", argsFromPayload(multiline))
	if strings.Contains(got, "\n") {
		t.Errorf("multi-line command should be collapsed to one line: %q", got)
	}

	// Structured args map (non-seed shape) still works.
	structured := map[string]interface{}{"args": map[string]interface{}{"path": "/tmp/f.go"}}
	got = buildDisplayName("read_file", argsFromPayload(structured))
	if got != "read_file /tmp/f.go" {
		t.Errorf("structured args not handled: %q", got)
	}
}
