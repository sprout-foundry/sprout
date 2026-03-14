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

	if len(optimized) >= len(messages) {
		t.Fatalf("Expected aggressive optimization to shrink message count, got %d -> %d", len(messages), len(optimized))
	}

	foundCompactSummary := false
	foundLegacyReadSummary := false
	for _, msg := range optimized {
		if containsString(msg.Content, "Compacted earlier conversation state:") {
			foundCompactSummary = true
		}
		if containsString(msg.Content, "Read file: pkg/foo.go") {
			foundLegacyReadSummary = true
		}
	}
	if !foundCompactSummary {
		t.Fatalf("Expected aggressive optimization to emit a compacted summary message")
	}
	if !foundLegacyReadSummary {
		t.Fatalf("Expected compacted summary to preserve the old file-read context")
	}
}

func TestCompactConversationRewritesOldMiddleHistory(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Fix the failing tests"},
		{Role: "assistant", Content: "I will inspect the repo and run the failing suite."},
	}

	for i := 0; i < 14; i++ {
		toolCallID := ""
		if i%2 == 0 {
			toolCallID = "call-old-" + string(rune('a'+i))
			messages = append(messages, api.Message{
				Role:    "assistant",
				Content: "",
				ToolCalls: []api.ToolCall{
					{ID: toolCallID},
				},
			})
			messages = append(messages, api.Message{
				Role:       "tool",
				ToolCallId: toolCallID,
				Content:    "Tool call result for read_file: pkg/foo.go\npackage foo\n\nfunc Example() {}\n",
			})
			continue
		}
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Updated tests and verified the package builds cleanly.",
		})
	}

	messages = append(messages,
		api.Message{Role: "user", Content: "Check the remaining failures"},
		api.Message{Role: "assistant", Content: "Looking at the latest failures now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call"}}},
		api.Message{Role: "tool", ToolCallId: "recent-call", Content: "Tool call result for shell_command: go test ./pkg/agent/...\nok"},
		api.Message{Role: "assistant", Content: "The recent failure is isolated to the new pruning path."},
	)

	compacted := optimizer.CompactConversation(messages)
	if len(compacted) >= len(messages) {
		t.Fatalf("expected compacted history to shrink message count, got %d -> %d", len(messages), len(compacted))
	}

	foundSummary := false
	foundRecentTool := false
	for _, msg := range compacted {
		if msg.Role == "assistant" && containsString(msg.Content, "Compacted earlier conversation state:") {
			foundSummary = true
		}
		if msg.Role == "tool" && msg.ToolCallId == "recent-call" {
			foundRecentTool = true
		}
	}

	if !foundSummary {
		t.Fatalf("expected compacted conversation summary message")
	}
	if !foundRecentTool {
		t.Fatalf("expected recent tool chain to remain intact")
	}

	if compacted[0].Role != "system" || compacted[1].Role != "user" {
		t.Fatalf("expected leading system/user anchor to remain intact")
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

func TestInvalidateFile(t *testing.T) {
	optimizer := NewConversationOptimizer(true, true) // Enable debug mode

	// Track a file read
	msg := api.Message{
		Role:    "tool",
		Content: "Tool call result for read_file: test.go\npackage main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}",
	}
	optimizer.trackFileRead(msg, 0)

	// Verify the file is tracked
	stats := optimizer.GetOptimizationStats()
	if stats["tracked_files"].(int) != 1 {
		t.Errorf("Expected 1 tracked file, got %d", stats["tracked_files"])
	}

	// Invalidate the file
	optimizer.InvalidateFile("test.go")

	// Verify the file is no longer tracked
	stats = optimizer.GetOptimizationStats()
	if stats["tracked_files"].(int) != 0 {
		t.Errorf("Expected 0 tracked files after invalidation, got %d", stats["tracked_files"])
	}

	// Verify invalidating a non-existent file doesn't cause issues
	optimizer.InvalidateFile("nonexistent.go")
	stats = optimizer.GetOptimizationStats()
	if stats["tracked_files"].(int) != 0 {
		t.Errorf("Expected 0 tracked files, got %d", stats["tracked_files"])
	}
}
