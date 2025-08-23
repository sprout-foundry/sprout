package tools

import (
	"context"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

func TestDuplicateRequestDetection(t *testing.T) {
	// Create a mock registry and executor
	registry := &mockRegistry{}
	logger := utils.GetLogger(true)
	cfg := &config.Config{}
	permissions := &SimplePermissionChecker{allowedPermissions: map[string]bool{
		"read_file": true,
	}}

	executor := NewExecutor(registry, permissions, logger, cfg)

	// Start a session
	sessionID := executor.StartSession()
	ctx := context.WithValue(context.Background(), "session_id", sessionID)

	// Create a read_file tool call
	toolCall := types.ToolCall{
		ID:   "test_1",
		Type: "function",
		Function: types.ToolCallFunction{
			Name:      "read_file",
			Arguments: `{"file_path": "/test/file.txt"}`,
		},
	}

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
		if !containsString(output, "has already been read") {
			t.Errorf("Expected 'already been read' message, got: %s", output)
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
