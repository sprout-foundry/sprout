package agent

import (
	"errors"
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

func TestCompactConversationPreservesLatestCompactedTaskContext(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Initial setup question"},
		{Role: "assistant", Content: "I will inspect the current implementation first."},
	}

	for i := 0; i < 10; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Reviewed the current implementation details and intermediate state.",
		})
	}

	activeTask := "Will the multi-instance workflow work safely without leaking state between folders?"
	messages = append(messages,
		api.Message{Role: "user", Content: activeTask},
		api.Message{Role: "assistant", Content: "I am tracing the multi-instance code paths and verifying isolation now."},
	)

	for i := 0; i < 14; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Verified another part of the instance-switching and workspace-isolation flow.",
		})
	}

	compacted := optimizer.CompactConversation(messages)
	if len(compacted) >= len(messages) {
		t.Fatalf("expected compacted history to shrink message count, got %d -> %d", len(messages), len(compacted))
	}

	var summary string
	for _, msg := range compacted {
		if msg.Role == "assistant" && containsString(msg.Content, "Compacted earlier conversation state:") {
			summary = msg.Content
			break
		}
	}

	if summary == "" {
		t.Fatalf("expected compacted conversation summary message")
	}
	if !containsString(summary, "Latest compacted user request: "+activeTask) {
		t.Fatalf("expected compacted summary to preserve the latest compacted user request, got: %s", summary)
	}
	if !containsString(summary, "Status at compaction time: work was still in progress") {
		t.Fatalf("expected compacted summary to mark the task as still in progress, got: %s", summary)
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

func TestCompactConversationWithLLMSummary(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	llmSummary := "The user asked to refactor auth module. Files auth.go and middleware.go were read and updated. Tests passed after the changes."
	scriptedClient := NewScriptedClient(
		NewScriptedResponseBuilder().Content(llmSummary).FinishReason("stop").Build(),
	)
	optimizer.SetLLMClient(scriptedClient, "test-provider", nil)

	// Build a conversation large enough to trigger compaction.
	// Requirements: ≥18 total messages, anchorEnd=3 (system + user + assistant),
	// recentStart at index len-12, and middle segment ≥6 messages.
	messages := []api.Message{
		{Role: "system", Content: "System prompt"},                        // 0 - anchor start
		{Role: "user", Content: "Refactor the auth module"},               // 1 - anchor user
		{Role: "assistant", Content: "I'll start by reviewing the code."}, // 2 - anchor assistant (no tool calls)
	}

	// Middle messages (indices 3 through 12 = 10 messages, well above MinMiddleMessages=6)
	for i := 0; i < 5; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Reviewed part of the auth implementation.",
		})
		messages = append(messages, api.Message{
			Role:    "user",
			Content: "Continue with the next part.",
		})
	}

	// Recent messages: 12 messages (indices 13-24). Total = 25 messages.
	// recentStart = 25 - 12 = 13 > anchorEnd(3), middle = 10 ≥ 6 → compaction triggers.
	messages = append(messages,
		api.Message{Role: "user", Content: "Check the remaining issues"},
		api.Message{Role: "assistant", Content: "Looking at the remaining issues now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-1"}}},
		api.Message{Role: "tool", ToolCallId: "recent-call-1", Content: "Tool call result for read_file: auth/token.go\npackage auth\n\nfunc Token() {}"},
		api.Message{Role: "assistant", Content: "Found the issue in token handling."},
		api.Message{Role: "user", Content: "Fix it please"},
		api.Message{Role: "assistant", Content: "Applying the fix now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-2"}}},
		api.Message{Role: "tool", ToolCallId: "recent-call-2", Content: "Tool call result for edit_file: auth/token.go\nok"},
		api.Message{Role: "assistant", Content: "Fix applied successfully."},
		api.Message{Role: "user", Content: "Run the tests"},
		api.Message{Role: "assistant", Content: "Running the test suite now."},
	)

	if len(messages) < 18 {
		t.Fatalf("test setup error: expected ≥18 messages, got %d", len(messages))
	}

	compacted := optimizer.CompactConversation(messages)

	// Compacted should be shorter than original (middle replaced by one summary message)
	if len(compacted) >= len(messages) {
		t.Fatalf("expected compacted history to shrink message count, got %d -> %d", len(messages), len(compacted))
	}

	// Verify the LLM was called exactly once
	sentRequests := scriptedClient.GetSentRequests()
	if len(sentRequests) != 1 {
		t.Fatalf("expected exactly 1 LLM call, got %d", len(sentRequests))
	}

	// The sent request should contain a system message with the summarizer prompt
	if len(sentRequests[0]) < 2 {
		t.Fatalf("expected at least 2 messages in the LLM request (system + user), got %d", len(sentRequests[0]))
	}
	if sentRequests[0][0].Role != "system" {
		t.Errorf("expected first message in LLM request to be system, got %s", sentRequests[0][0].Role)
	}

	// Find the summary message in compacted output
	var summaryMsg *api.Message
	for i := range compacted {
		if compacted[i].Role == "assistant" && containsString(compacted[i].Content, "Compacted earlier conversation state:") {
			summaryMsg = &compacted[i]
			break
		}
	}
	if summaryMsg == nil {
		t.Fatalf("expected compacted conversation to contain a summary message starting with 'Compacted earlier conversation state:'")
	}

	// The summary should contain the LLM-generated text
	if !containsString(summaryMsg.Content, llmSummary) {
		t.Errorf("expected summary to contain LLM-generated text, got: %s", summaryMsg.Content)
	}

	// First message should be the system message (anchor preserved)
	if compacted[0].Role != "system" {
		t.Errorf("expected first message to be system, got %s", compacted[0].Role)
	}

	// Second message should still be the user anchor
	if compacted[1].Role != "user" {
		t.Errorf("expected second message to be user anchor, got %s", compacted[1].Role)
	}

	// The recent tool chain should remain intact
	foundRecentTool := false
	for _, msg := range compacted {
		if msg.Role == "tool" && msg.ToolCallId == "recent-call-2" {
			foundRecentTool = true
			break
		}
	}
	if !foundRecentTool {
		t.Fatal("expected recent tool result (recent-call-2) to remain intact")
	}
}

func TestCompactConversationLLMErrorFallsBackToGoSummary(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	// Script the LLM to return an error, forcing fallback to Go-based summary
	scriptedClient := NewScriptedClient(
		NewErrorResponse(errors.New("LLM unavailable")),
	)
	optimizer.SetLLMClient(scriptedClient, "test-provider", nil)

	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Refactor the auth module"},
		{Role: "assistant", Content: "I'll start by reviewing the code."},
	}

	// sufficiently large middle segment
	for i := 0; i < 5; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Reviewed implementation details for the auth flow.",
		})
		messages = append(messages, api.Message{
			Role:    "user",
			Content: "Continue with the next part.",
		})
	}

	// 12 recent messages
	messages = append(messages,
		api.Message{Role: "user", Content: "Check the remaining issues"},
		api.Message{Role: "assistant", Content: "Looking at the remaining issues now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-fb"}}},
		api.Message{Role: "tool", ToolCallId: "recent-call-fb", Content: "Tool call result for shell_command: go test ./...\nok"},
		api.Message{Role: "assistant", Content: "Tests are passing."},
		api.Message{Role: "user", Content: "Great, wrap up."},
		api.Message{Role: "assistant", Content: "Wrapping up the session."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-fb2"}}},
		api.Message{Role: "tool", ToolCallId: "recent-call-fb2", Content: "Tool call result for shell_command: go build ./...\nok"},
		api.Message{Role: "assistant", Content: "Build succeeded."},
		api.Message{Role: "user", Content: "Done"},
		api.Message{Role: "assistant", Content: "All done."},
	)

	if len(messages) < 18 {
		t.Fatalf("test setup error: expected ≥18 messages, got %d", len(messages))
	}

	compacted := optimizer.CompactConversation(messages)

	if len(compacted) >= len(messages) {
		t.Fatalf("expected compacted history to shrink message count, got %d -> %d", len(messages), len(compacted))
	}

	// Verify the LLM was called (and failed)
	sentRequests := scriptedClient.GetSentRequests()
	if len(sentRequests) != 1 {
		t.Fatalf("expected exactly 1 LLM call attempt, got %d", len(sentRequests))
	}

	// The Go fallback should still produce a wrapped summary with the standard header
	foundSummary := false
	for _, msg := range compacted {
		if msg.Role == "assistant" && containsString(msg.Content, "Compacted earlier conversation state:") {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatal("expected fallback Go summary to contain 'Compacted earlier conversation state:' header")
	}

	// Anchor and recent messages should be preserved
	if compacted[0].Role != "system" {
		t.Errorf("expected first message to be system, got %s", compacted[0].Role)
	}
	if compacted[1].Role != "user" {
		t.Errorf("expected second message to be user anchor, got %s", compacted[1].Role)
	}

	foundRecentTool := false
	for _, msg := range compacted {
		if msg.Role == "tool" && msg.ToolCallId == "recent-call-fb2" {
			foundRecentTool = true
			break
		}
	}
	if !foundRecentTool {
		t.Fatal("expected recent tool result to remain intact in fallback path")
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
