package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestConversationOptimizer(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	// Create a simpler test that verifies the new behavior:
	// Recent file reads (within 10 messages) should NOT be optimized
	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "tool", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}"},
		{Role: "assistant", Content: "Working..."},
		{Role: "tool", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}"},
	}

	optimized := optimizer.OptimizeConversation(messages)

	// With the new logic, recent reads should NOT be optimized (gap is only 2 messages, less than 10)
	if len(optimized) != len(messages) {
		t.Errorf("Expected no optimization for recent reads, got %d -> %d", len(messages), len(optimized))
	}

	// Verify stats show tracked files but no optimization
	stats := optimizer.GetOptimizationStats()
	if stats["tracked_files"].(int) == 0 {
		t.Errorf("Expected tracked files > 0, got %d", stats["tracked_files"])
	}
}

func TestConversationOptimizerWithOldReads(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	// Create test with file reads that are far apart (>= 15 messages)
	// The FIRST read should be optimized, the LAST read should be preserved
	messages := []api.Message{
		{Role: "system", Content: "System prompt"}, // index 0
		{Role: "tool", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}", ToolCallId: "call-1"}, // index 1 - FIRST read (should be optimized)
		{Role: "assistant", Content: "Message 2"},  // index 2
		{Role: "user", Content: "Message 3"},       // index 3
		{Role: "assistant", Content: "Message 4"},  // index 4
		{Role: "user", Content: "Message 5"},       // index 5
		{Role: "assistant", Content: "Message 6"},  // index 6
		{Role: "user", Content: "Message 7"},       // index 7
		{Role: "assistant", Content: "Message 8"},  // index 8
		{Role: "user", Content: "Message 9"},       // index 9
		{Role: "assistant", Content: "Message 10"}, // index 10
		{Role: "user", Content: "Message 11"},      // index 11
		{Role: "assistant", Content: "Message 12"}, // index 12
		{Role: "user", Content: "Message 13"},      // index 13
		{Role: "assistant", Content: "Message 14"}, // index 14
		{Role: "user", Content: "Message 15"},      // index 15
		{Role: "assistant", Content: "Message 16"}, // index 16
		{Role: "tool", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}", ToolCallId: "call-2"}, // index 17 - LAST read (should be preserved)
	}

	optimized := optimizer.OptimizeConversation(messages)

	// The message count should stay the same (optimization replaces content, doesn't remove messages)
	if len(optimized) != len(messages) {
		t.Errorf("Expected same message count after optimization, got %d -> %d", len(messages), len(optimized))
	}

	// Check that the FIRST file read was optimized (index 1)
	firstReadMsg := optimized[1]
	if !containsString(firstReadMsg.Content, "[OPTIMIZED]") {
		t.Errorf("Expected first read (index 1) to contain [OPTIMIZED], got: %s", firstReadMsg.Content)
	}
	if firstReadMsg.ToolCallId != "call-1" {
		t.Errorf("Expected first read (index 1) to preserve ToolCallId, got: %s", firstReadMsg.ToolCallId)
	}

	// Check that the LAST file read was preserved (index 17)
	lastReadMsg := optimized[17]
	if containsString(lastReadMsg.Content, "[OPTIMIZED]") {
		t.Errorf("Expected last read (index 17) to NOT contain [OPTIMIZED], got: %s", lastReadMsg.Content)
	}
	if lastReadMsg.ToolCallId != "call-2" {
		t.Errorf("Expected last read (index 17) to preserve ToolCallId, got: %s", lastReadMsg.ToolCallId)
	}

}

func TestAggressiveOptimizationPreservesToolCallId(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	// Create a longer conversation to trigger aggressive optimization
	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Initial question"},
		{Role: "tool", Content: "Tool call result for read_file: pkg/foo.go\npackage foo", ToolCallId: "agg-call"},
		{Role: "assistant", Content: "Message 3"},
		{Role: "user", Content: "Message 4"},
		{Role: "assistant", Content: "Message 5"},
		{Role: "user", Content: "Message 6"},
		{Role: "assistant", Content: "Message 7"},
		{Role: "user", Content: "Message 8"},
		{Role: "assistant", Content: "Message 9"},
		{Role: "user", Content: "Message 10"},
		{Role: "assistant", Content: "Message 11"},
		{Role: "user", Content: "Message 12"},
		{Role: "assistant", Content: "Message 13"},
		{Role: "user", Content: "Message 14"},
		{Role: "assistant", Content: "Message 15"},
		{Role: "user", Content: "Message 16"},
		{Role: "assistant", Content: "Message 17"},
		{Role: "user", Content: "Message 18"},
		{Role: "assistant", Content: "Message 19"},
		{Role: "user", Content: "Message 20"},
	}

	optimized := optimizer.AggressiveOptimization(messages)

	if len(optimized) != len(messages) {
		t.Fatalf("Expected same message count after aggressive optimization, got %d -> %d", len(messages), len(optimized))
	}

	rewritten := optimized[2]
	if !containsString(rewritten.Content, "[COMPACT]") {
		t.Fatalf("Expected aggressive summary to contain [COMPACT], got: %s", rewritten.Content)
	}
	if rewritten.ToolCallId != "agg-call" {
		t.Fatalf("Expected aggressive summary to preserve ToolCallId, got: %s", rewritten.ToolCallId)
	}
}

func TestFileReadDetection(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	// Test file path extraction
	content := "Tool call result for read_file: agent/agent.go\npackage agent\n\nfunc main() {}"
	filePath := optimizer.extractFilePath(content)
	if filePath != "agent/agent.go" {
		t.Errorf("Expected file path 'agent/agent.go', got '%s'", filePath)
	}

	// Test file content extraction
	fileContent := optimizer.extractFileContent(content)
	expected := "package agent\n\nfunc main() {}"
	if fileContent != expected {
		t.Errorf("Expected file content '%s', got '%s'", expected, fileContent)
	}
}

func TestOptimizationDisabled(t *testing.T) {
	optimizer := NewConversationOptimizer(false, false)

	messages := []api.Message{
		{Role: "tool", Content: "Tool call result for read_file: test.go\ncontent"},
		{Role: "tool", Content: "Tool call result for read_file: test.go\ncontent"},
	}

	optimized := optimizer.OptimizeConversation(messages)

	// Should not optimize when disabled
	if len(optimized) != len(messages) {
		t.Errorf("Expected no optimization when disabled, got %d -> %d", len(messages), len(optimized))
	}
}

func TestFileContentChange(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	messages := []api.Message{
		{Role: "tool", Content: "Tool call result for read_file: test.go\noriginal content"},
		{Role: "tool", Content: "Tool call result for read_file: test.go\nmodified content"},
	}

	optimized := optimizer.OptimizeConversation(messages)

	// Should not optimize if content changed
	if len(optimized) != len(messages) {
		t.Errorf("Expected no optimization when content changed, got %d -> %d", len(messages), len(optimized))
	}
}

func TestCreateFileReadSummary(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	msg := api.Message{
		Role:    "tool",
		Content: "Tool call result for read_file: test.go\npackage main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}",
	}

	summary := optimizer.createFileReadSummary(msg)

	if !containsString(summary, "[OPTIMIZED]") {
		t.Errorf("Expected summary to contain [OPTIMIZED], got: %s", summary)
	}

	if !containsString(summary, "test.go") {
		t.Errorf("Expected summary to contain file path, got: %s", summary)
	}

	if !containsString(summary, "Go file") {
		t.Errorf("Expected summary to identify Go file type, got: %s", summary)
	}
}

// Helper function to check if string contains substring
func containsString(text, substr string) bool {
	return len(text) >= len(substr) && findSubstring(text, substr) != -1
}

// Simple substring search
func findSubstring(text, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	for i := 0; i <= len(text)-len(substr); i++ {
		if text[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
