package agent

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

func TestConversationOptimizer(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	// Create a simpler test that verifies the new behavior:
	// Recent file reads (within 10 messages) should NOT be optimized
	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}"},
		{Role: "assistant", Content: "Working..."},
		{Role: "user", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}"},
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

	// Create test with file reads that are far apart (>= 5 messages)
	// The FIRST read should be optimized, the LAST read should be preserved
	messages := []api.Message{
		{Role: "system", Content: "System prompt"}, // index 0
		{Role: "user", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}"}, // index 1 - FIRST read (should be optimized)
		{Role: "assistant", Content: "Message 2"}, // index 2
		{Role: "user", Content: "Message 3"}, // index 3
		{Role: "assistant", Content: "Message 4"}, // index 4
		{Role: "user", Content: "Message 5"}, // index 5
		{Role: "assistant", Content: "Message 6"}, // index 6
		{Role: "user", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}"}, // index 7 - LAST read (should be preserved)
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

	// Check that the LAST file read was preserved (index 7)
	lastReadMsg := optimized[7]
	if containsString(lastReadMsg.Content, "[OPTIMIZED]") {
		t.Errorf("Expected last read (index 7) to NOT contain [OPTIMIZED], got: %s", lastReadMsg.Content)
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
		{Role: "user", Content: "Tool call result for read_file: test.go\ncontent"},
		{Role: "user", Content: "Tool call result for read_file: test.go\ncontent"},
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
		{Role: "user", Content: "Tool call result for read_file: test.go\noriginal content"},
		{Role: "user", Content: "Tool call result for read_file: test.go\nmodified content"},
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
		Role:    "user",
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