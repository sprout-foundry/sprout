package tools

import (
	"context"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

func TestDuplicateRequestDetection(t *testing.T) {
	// Create a mock registry and executor
	registry := &mockRegistry{}
	logger := utils.GetLogger(true)
	cfg := &configuration.Config{}
	permissions := &SimplePermissionChecker{allowedPermissions: map[string]bool{
		"read_file": true,
	}}

	executor := NewExecutor(registry, permissions, logger, cfg)

	// Start a session
	sessionID := executor.StartSession()
	ctx := context.WithValue(context.Background(), "session_id", sessionID)

	// Create a read_file tool call
	toolCall := api.ToolCall{
		ID:   "test_1",
		Type: "function",
	}
	toolCall.Function.Name = "read_file"
	toolCall.Function.Arguments = `{"file_path": "/test/file.txt"}`

	// First call should succeed (assuming the mock tool returns successfully)
	// We need to register a mock tool first
	registry.tools = []Tool{&mockTool{}}

	t.Logf("Session ID: %s", sessionID)
	result1, err1 := executor.ExecuteToolCall(ctx, toolCall)
	if err1 != nil {
		t.Errorf("First tool call failed: %v", err1)
	}
	if result1 == nil || !result1.Success {
		t.Error("First tool call should succeed")
	}

	// Check session stats after first call
	stats1 := executor.GetSessionStats(sessionID)
	t.Logf("Session stats after first call: %+v", stats1)

	// Second call to the same file should be detected as duplicate
	result2, err2 := executor.ExecuteToolCall(ctx, toolCall)
	if err2 != nil {
		t.Errorf("Second tool call failed: %v", err2)
	}
	if result2 == nil || !result2.Success {
		t.Error("Second tool call should succeed (with duplicate message)")
	}

	// Check that the result indicates it's a duplicate request
	if output, ok := result2.Output.(string); ok {
		if !containsString(output, "Duplicate request detected") {
			t.Errorf("Expected duplicate message, got: %s", output)
		}
		if !containsString(output, "You already have this file content available") {
			t.Errorf("Expected 'already have this file content' message, got: %s", output)
		}
	} else {
		t.Error("Expected string output for duplicate detection")
	}

	// Check metadata
	if result2.Metadata != nil {
		if duplicate, ok := result2.Metadata["duplicate_request"].(bool); ok && !duplicate {
			t.Error("Expected duplicate_request to be true")
		}
		if callCount, ok := result2.Metadata["call_count"].(int); ok && callCount != 1 {
			t.Errorf("Expected call_count to be 1 (current count at time of duplicate), got %d", callCount)
		}
	}
}

func TestDuplicateRequestDetectionRespectsLineRanges(t *testing.T) {
	registry := &mockRegistry{}
	logger := utils.GetLogger(true)
	cfg := &configuration.Config{}
	permissions := &SimplePermissionChecker{allowedPermissions: map[string]bool{
		"read_file": true,
	}}

	executor := NewExecutor(registry, permissions, logger, cfg)
	registry.tools = []Tool{&mockTool{}}

	sessionID := executor.StartSession()
	ctx := context.WithValue(context.Background(), "session_id", sessionID)

	toolCall := api.ToolCall{ID: "range_test", Type: "function"}
	toolCall.Function.Name = "read_file"

	// First ranged read
	toolCall.Function.Arguments = `{"file_path": "/test/ranged.txt", "start_line": 1, "end_line": 5}`
	result1, err1 := executor.ExecuteToolCall(ctx, toolCall)
	if err1 != nil {
		t.Fatalf("First ranged read failed: %v", err1)
	}
	if result1 == nil || !result1.Success {
		t.Fatal("First ranged read should succeed")
	}

	// Duplicate ranged read should be detected
	result2, err2 := executor.ExecuteToolCall(ctx, toolCall)
	if err2 != nil {
		t.Fatalf("Duplicate ranged read failed: %v", err2)
	}
	if result2 == nil || !result2.Success {
		t.Fatal("Duplicate ranged read should return success with warning")
	}
	if output, ok := result2.Output.(string); ok {
		if !containsString(output, "Duplicate request detected") {
			t.Errorf("Expected duplicate warning, got: %s", output)
		}
	} else {
		t.Fatal("Expected string output for duplicate detection")
	}

	// Different range should not be treated as duplicate
	toolCall.Function.Arguments = `{"file_path": "/test/ranged.txt", "start_line": 6, "end_line": 10}`
	result3, err3 := executor.ExecuteToolCall(ctx, toolCall)
	if err3 != nil {
		t.Fatalf("Second ranged read failed: %v", err3)
	}
	if result3 == nil || !result3.Success {
		t.Fatal("Second ranged read should succeed")
	}
	if output, ok := result3.Output.(string); ok {
		if containsString(output, "Duplicate request detected") {
			t.Errorf("Did not expect duplicate warning for different range, got: %s", output)
		}
	} else {
		t.Fatal("Expected string output for ranged read")
	}

	if result3.Metadata != nil {
		if duplicate, ok := result3.Metadata["duplicate_request"].(bool); ok && duplicate {
			t.Error("Expected duplicate_request metadata to be false for different range")
		}
	}
}

func TestExecuteToolCallInvalidJSON(t *testing.T) {
	registry := &mockRegistry{}
	logger := utils.GetLogger(true)
	cfg := &configuration.Config{}
	permissions := &SimplePermissionChecker{allowedPermissions: map[string]bool{}}

	executor := NewExecutor(registry, permissions, logger, cfg)

	toolCall := api.ToolCall{ID: "invalid_json", Type: "function"}
	toolCall.Function.Name = "read_file"
	toolCall.Function.Arguments = "{this is not valid json}"

	result, err := executor.ExecuteToolCall(context.Background(), toolCall)
	if err != nil {
		t.Fatalf("expected no execution error, got %v", err)
	}
	if result == nil {
		t.Fatal("expected result describing failure")
	}
	if result.Success {
		t.Error("expected success=false when JSON parsing fails")
	}
	if len(result.Errors) == 0 {
		t.Error("expected error message for JSON parsing failure")
	}
}

func TestExecuteToolCallToolNotFound(t *testing.T) {
	registry := &mockRegistry{}
	logger := utils.GetLogger(true)
	cfg := &configuration.Config{}
	permissions := &SimplePermissionChecker{allowedPermissions: map[string]bool{"any": true}}

	executor := NewExecutor(registry, permissions, logger, cfg)

	toolCall := api.ToolCall{ID: "missing_tool", Type: "function"}
	toolCall.Function.Name = "nonexistent_tool"
	toolCall.Function.Arguments = `{"foo": "bar"}`

	result, err := executor.ExecuteToolCall(context.Background(), toolCall)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil {
		t.Fatal("expected result describing missing tool")
	}
	if result.Success {
		t.Error("expected success=false when tool is not registered")
	}
	if len(result.Errors) == 0 || !containsString(result.Errors[0], "not found") {
		t.Errorf("expected not found error, got %v", result.Errors)
	}
}

func TestExecuteToolCallPermissionDenied(t *testing.T) {
	registry := &mockRegistry{}
	logger := utils.GetLogger(true)
	cfg := &configuration.Config{}
	permissions := &SimplePermissionChecker{allowedPermissions: map[string]bool{}}

	restricted := &restrictedTool{}
	registry.tools = []Tool{restricted}

	executor := NewExecutor(registry, permissions, logger, cfg)

	toolCall := api.ToolCall{ID: "permission_test", Type: "function"}
	toolCall.Function.Name = restricted.Name()
	toolCall.Function.Arguments = `{"target_file": "foo.txt"}`

	result, err := executor.ExecuteToolCall(context.Background(), toolCall)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil || result.Success {
		t.Fatal("expected failure result when permissions missing")
	}
	if restricted.executed {
		t.Error("restricted tool should not execute when permissions are denied")
	}
	if len(result.Errors) == 0 || !containsString(result.Errors[0], "insufficient permissions") {
		t.Errorf("expected insufficient permissions error, got %v", result.Errors)
	}
}

func TestExecuteToolCallNormalizesAliases(t *testing.T) {
	registry := &mockRegistry{}
	logger := utils.GetLogger(true)
	cfg := &configuration.Config{}
	permissions := &SimplePermissionChecker{allowedPermissions: map[string]bool{"read_file": true}}

	capturing := &capturingTool{}
	shellCapturing := &shellCapturingTool{}
	registry.tools = []Tool{capturing, shellCapturing}

	executor := NewExecutor(registry, permissions, logger, cfg)

	sessionID := executor.StartSession()
	ctx := context.WithValue(context.Background(), "session_id", sessionID)

	readCall := api.ToolCall{ID: "alias_test_read", Type: "function"}
	readCall.Function.Name = capturing.Name()
	readCall.Function.Arguments = `{"target_file": "alias.txt"}`

	readResult, readErr := executor.ExecuteToolCall(ctx, readCall)
	if readErr != nil {
		t.Fatalf("expected no error, got %v", readErr)
	}
	if readResult == nil || !readResult.Success {
		t.Fatalf("expected successful read execution, got %+v", readResult)
	}
	if capturing.lastParams == nil {
		t.Fatal("expected read tool Execute to be invoked")
	}
	if fp, ok := capturing.lastParams.Kwargs["file_path"].(string); !ok || fp != "alias.txt" {
		t.Errorf("expected file_path normalized to alias.txt, got %v", capturing.lastParams.Kwargs["file_path"])
	}

	shellCall := api.ToolCall{ID: "alias_test_shell", Type: "function"}
	shellCall.Function.Name = shellCapturing.Name()
	shellCall.Function.Arguments = `{"cmd": "ls -la"}`

	shellResult, shellErr := executor.ExecuteToolCall(ctx, shellCall)
	if shellErr != nil {
		t.Fatalf("expected no error, got %v", shellErr)
	}
	if shellResult == nil || !shellResult.Success {
		t.Fatalf("expected successful shell execution, got %+v", shellResult)
	}
	if shellCapturing.lastParams == nil {
		t.Fatal("expected shell tool Execute to be invoked")
	}
	if cmd, ok := shellCapturing.lastParams.Kwargs["command"].(string); !ok || cmd != "ls -la" {
		t.Errorf("expected command normalized to ls -la, got %v", shellCapturing.lastParams.Kwargs["command"])
	}
}

// Mock registry for testing
type mockRegistry struct {
	tools []Tool
}

func (m *mockRegistry) RegisterTool(tool Tool) error {
	m.tools = append(m.tools, tool)
	return nil
}

func (m *mockRegistry) GetTool(name string) (Tool, bool) {
	for _, tool := range m.tools {
		if tool.Name() == name {
			return tool, true
		}
	}
	return nil, false
}

func (m *mockRegistry) UnregisterTool(name string) error {
	return nil
}

func (m *mockRegistry) ListTools() []Tool {
	return m.tools
}

func (m *mockRegistry) ListToolsByCategory(category string) []Tool {
	return m.tools
}

// Mock tool for testing
type mockTool struct{}

func (m *mockTool) Name() string {
	return "read_file"
}

func (m *mockTool) Description() string {
	return "Mock read_file tool"
}

func (m *mockTool) Category() string {
	return "file"
}

func (m *mockTool) Execute(ctx context.Context, params Parameters) (*Result, error) {
	filePath, _ := params.Kwargs["file_path"].(string)
	content := "Mock content for " + filePath

	return &Result{
		Success: true,
		Output:  content,
		Metadata: map[string]interface{}{
			"file_path": filePath,
		},
	}, nil
}

func (m *mockTool) CanExecute(ctx context.Context, params Parameters) bool {
	return true
}

func (m *mockTool) RequiredPermissions() []string {
	return []string{"read_file"}
}

func (m *mockTool) EstimatedDuration() time.Duration {
	return 0
}

func (m *mockTool) IsAvailable() bool {
	return true
}

type restrictedTool struct {
	executed bool
}

func (r *restrictedTool) Name() string {
	return "restricted_tool"
}

func (r *restrictedTool) Description() string {
	return "Restricted tool"
}

func (r *restrictedTool) Category() string {
	return "misc"
}

func (r *restrictedTool) Execute(ctx context.Context, params Parameters) (*Result, error) {
	r.executed = true
	return &Result{Success: true, Output: "ok"}, nil
}

func (r *restrictedTool) CanExecute(ctx context.Context, params Parameters) bool {
	return true
}

func (r *restrictedTool) RequiredPermissions() []string {
	return []string{"write_file"}
}

func (r *restrictedTool) EstimatedDuration() time.Duration {
	return 0
}

func (r *restrictedTool) IsAvailable() bool {
	return true
}

type capturingTool struct {
	lastParams *Parameters
}

func (c *capturingTool) Name() string {
	return "read_file"
}

func (c *capturingTool) Description() string {
	return "Capturing tool"
}

func (c *capturingTool) Category() string {
	return "file"
}

func (c *capturingTool) Execute(ctx context.Context, params Parameters) (*Result, error) {
	paramsCopy := params
	c.lastParams = &paramsCopy
	return &Result{Success: true, Output: "captured"}, nil
}

func (c *capturingTool) CanExecute(ctx context.Context, params Parameters) bool {
	return true
}

func (c *capturingTool) RequiredPermissions() []string {
	return []string{"read_file"}
}

func (c *capturingTool) EstimatedDuration() time.Duration {
	return 0
}

func (c *capturingTool) IsAvailable() bool {
	return true
}

type shellCapturingTool struct {
	lastParams *Parameters
}

func (c *shellCapturingTool) Name() string {
	return "run_shell_command"
}

func (c *shellCapturingTool) Description() string {
	return "Shell capturing tool"
}

func (c *shellCapturingTool) Category() string {
	return "shell"
}

func (c *shellCapturingTool) Execute(ctx context.Context, params Parameters) (*Result, error) {
	paramsCopy := params
	c.lastParams = &paramsCopy
	return &Result{Success: true, Output: "shell"}, nil
}

func (c *shellCapturingTool) CanExecute(ctx context.Context, params Parameters) bool {
	return true
}

func (c *shellCapturingTool) RequiredPermissions() []string {
	return nil
}

func (c *shellCapturingTool) EstimatedDuration() time.Duration {
	return 0
}

func (c *shellCapturingTool) IsAvailable() bool {
	return true
}

// Helper function to check if a string contains a substring (from session_tracker.go)
func containsString(str, substr string) bool {
	return len(str) >= len(substr) && (str == substr || len(substr) == 0 ||
		len(str) > len(substr) && (str[:len(substr)] == substr || str[len(str)-len(substr):] == substr ||
			func() bool {
				for i := 1; i <= len(str)-len(substr); i++ {
					if str[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}()))
}
