package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
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
		{Role: "tool", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}", ToolCallID: "call-1"}, // index 1 - FIRST read (should be optimized)
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
		{Role: "tool", Content: "Tool call result for read_file: agent/agent.go\npackage agent\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}", ToolCallID: "call-2"}, // index 17 - LAST read (should be preserved)
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
	if firstReadMsg.ToolCallID != "call-1" {
		t.Errorf("Expected first read (index 1) to preserve ToolCallId, got: %s", firstReadMsg.ToolCallID)
	}

	// Check that the LAST file read was preserved (index 17)
	lastReadMsg := optimized[17]
	if containsString(lastReadMsg.Content, "[OPTIMIZED]") {
		t.Errorf("Expected last read (index 17) to NOT contain [OPTIMIZED], got: %s", lastReadMsg.Content)
	}
	if lastReadMsg.ToolCallID != "call-2" {
		t.Errorf("Expected last read (index 17) to preserve ToolCallId, got: %s", lastReadMsg.ToolCallID)
	}

}

func TestCompactConversationRewritesOldMiddleHistory(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Fix the failing tests"},
		{Role: "assistant", Content: "I will inspect the repo and run the failing suite."},
	}

	// Middle segment: 16 messages (8 tool call pairs)
	for i := 0; i < 8; i++ {
		toolCallID := "call-old-" + string(rune('a'+i))
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "",
			ToolCalls: []api.ToolCall{
				{ID: toolCallID},
			},
		})
		messages = append(messages, api.Message{
			Role:       "tool",
			ToolCallID: toolCallID,
			Content:    "Tool call result for read_file: pkg/foo.go\npackage foo\n\nfunc Example() {}\n",
		})
	}

	// Recent segment: 24 messages to match RecentMessagesToKeep
	for i := 0; i < 12; i++ {
		messages = append(messages,
			api.Message{Role: "user", Content: fmt.Sprintf("Check issue %d", i)},
			api.Message{Role: "assistant", Content: fmt.Sprintf("Looking at issue %d", i)},
		)
	}

	compacted := optimizer.CompactConversation(messages)
	if len(compacted) >= len(messages) {
		t.Fatalf("expected compacted history to shrink message count, got %d -> %d", len(messages), len(compacted))
	}

	foundSummary := false
	for _, msg := range compacted {
		if msg.Role == "assistant" && containsString(msg.Content, "Compacted earlier conversation state:") {
			foundSummary = true
		}
	}

	if !foundSummary {
		t.Fatalf("expected compacted conversation summary message")
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

	// Middle segment: 16 messages
	for i := 0; i < 8; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Reviewed the current implementation details and intermediate state.",
		})
		messages = append(messages, api.Message{
			Role:    "user",
			Content: "Continue with the next part.",
		})
	}

	activeTask := "Will the multi-instance workflow work safely without leaking state between folders?"
	messages = append(messages,
		api.Message{Role: "user", Content: activeTask},
		api.Message{Role: "assistant", Content: "I am tracing the multi-instance code paths and verifying isolation now."},
	)

	// Recent segment: 24 messages to match RecentMessagesToKeep
	for i := 0; i < 12; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Verified another part of the instance-switching and workspace-isolation flow.",
		})
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Check part %d", i),
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
	// Requirements: ≥30 total messages (MinMessagesToCompact), anchorEnd=3 (system + user + assistant),
	// recentStart at index len-24, and middle segment ≥6 messages.
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

	// Recent messages: 24 messages (indices 13-36). Total = 37 messages.
	// recentStart = 37 - 24 = 13 > anchorEnd(3), middle = 10 ≥ 6 → compaction triggers.
	messages = append(messages,
		api.Message{Role: "user", Content: "Check the remaining issues"},
		api.Message{Role: "assistant", Content: "Looking at the remaining issues now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-1"}}},
		api.Message{Role: "tool", ToolCallID: "recent-call-1", Content: "Tool call result for read_file: auth/token.go\npackage auth\n\nfunc Token() {}"},
		api.Message{Role: "assistant", Content: "Found the issue in token handling."},
		api.Message{Role: "user", Content: "Fix it please"},
		api.Message{Role: "assistant", Content: "Applying the fix now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-2"}}},
		api.Message{Role: "tool", ToolCallID: "recent-call-2", Content: "Tool call result for edit_file: auth/token.go\nok"},
		api.Message{Role: "assistant", Content: "Fix applied successfully."},
		api.Message{Role: "user", Content: "Run the tests"},
		api.Message{Role: "assistant", Content: "Running the test suite now."},
		// Add more recent messages to reach 24 recent messages
		api.Message{Role: "user", Content: "Are all tests passing?"},
		api.Message{Role: "assistant", Content: "Yes, all tests are passing."},
		api.Message{Role: "user", Content: "Good, let me check the build"},
		api.Message{Role: "assistant", Content: "Building the project now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-3"}}},
		api.Message{Role: "tool", ToolCallID: "recent-call-3", Content: "Tool call result for shell_command: go build ./...\nok"},
		api.Message{Role: "assistant", Content: "Build succeeded."},
		api.Message{Role: "user", Content: "Check the linting"},
		api.Message{Role: "assistant", Content: "Running linter now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-4"}}},
		api.Message{Role: "tool", ToolCallID: "recent-call-4", Content: "Tool call result for shell_command: golangci-lint run\nno issues found"},
		api.Message{Role: "assistant", Content: "No linting issues found."},
		api.Message{Role: "user", Content: "Great, what's next?"},
		api.Message{Role: "assistant", Content: "The refactoring is complete."},
	)

	if len(messages) < 30 {
		t.Fatalf("test setup error: expected ≥30 messages, got %d", len(messages))
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
		if msg.Role == "tool" && msg.ToolCallID == "recent-call-2" {
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

	// 24 recent messages
	messages = append(messages,
		api.Message{Role: "user", Content: "Check the remaining issues"},
		api.Message{Role: "assistant", Content: "Looking at the remaining issues now."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-fb"}}},
		api.Message{Role: "tool", ToolCallID: "recent-call-fb", Content: "Tool call result for shell_command: go test ./...\nok"},
		api.Message{Role: "assistant", Content: "Tests are passing."},
		api.Message{Role: "user", Content: "Great, wrap up."},
		api.Message{Role: "assistant", Content: "Wrapping up the session."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-fb2"}}},
		api.Message{Role: "tool", ToolCallID: "recent-call-fb2", Content: "Tool call result for shell_command: go build ./...\nok"},
		api.Message{Role: "assistant", Content: "Build succeeded."},
		api.Message{Role: "user", Content: "Done"},
		api.Message{Role: "assistant", Content: "All done."},
		// Add more recent messages to reach 24
		api.Message{Role: "user", Content: "Verify the changes"},
		api.Message{Role: "assistant", Content: "Verifying changes now."},
		api.Message{Role: "user", Content: "Check documentation"},
		api.Message{Role: "assistant", Content: "Documentation looks good."},
		api.Message{Role: "user", Content: "Run integration tests"},
		api.Message{Role: "assistant", Content: "Running integration tests."},
		api.Message{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "recent-call-fb3"}}},
		api.Message{Role: "tool", ToolCallID: "recent-call-fb3", Content: "Tool call result for shell_command: go test -race ./... ok"},
		api.Message{Role: "assistant", Content: "Integration tests passed."},
		api.Message{Role: "user", Content: "Final review"},
		api.Message{Role: "assistant", Content: "Final review complete."},
	)

	if len(messages) < 30 {
		t.Fatalf("test setup error: expected ≥30 messages, got %d", len(messages))
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
		if msg.Role == "tool" && msg.ToolCallID == "recent-call-fb2" {
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

func TestCompactConversationLayered(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	// Build a conversation with a large middle segment (>= 30 messages) to trigger layered compaction.
	// Layout:
	//   [0]  system  - anchor start
	//   [1]  user    - anchor user
	//   [2]  assistant - anchor assistant (no tool calls)
	//   [3..62]  60 middle messages (well above LayeredThreshold=30)
	//   [63..86] 24 recent messages (RecentMessagesToKeep=24)
	// Total = 87 messages

	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Implement a complex multi-file refactoring"},
		{Role: "assistant", Content: "I'll start by reviewing the codebase."},
	}

	// Middle segment: 60 messages (30 user/assistant pairs)
	for i := 0; i < 30; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Continue with step %d of the refactoring, checking error handling and edge cases", i),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Reviewed and updated component %d, verified the changes build cleanly", i),
		})
	}

	// Recent segment: 24 messages (12 user/assistant pairs)
	for i := 0; i < 12; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Check remaining issue %d", i),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Looking at issue %d", i),
		})
	}

	total := len(messages)
	if total < PruningConfig.Structural.MinMessagesToCompact {
		t.Fatalf("test setup error: need >= %d messages, got %d", PruningConfig.Structural.MinMessagesToCompact, total)
	}

	compacted := optimizer.CompactConversation(messages)

	// Verify compaction happened
	if len(compacted) >= len(messages) {
		t.Fatalf("expected compaction to reduce messages: got %d, original %d", len(compacted), len(messages))
	}

	// Verify a single merged summary message was created (layered compaction now merges all layers)
	summaryCount := 0
	for _, msg := range compacted {
		if msg.Role == "assistant" && strings.Contains(msg.Content, "[Context compaction — layered summary]") {
			summaryCount++
		}
	}
	if summaryCount != 1 {
		t.Fatalf("expected exactly 1 merged layered summary message, got %d", summaryCount)
	}

	// Verify the merged summary contains references to all three detail levels
	for _, msg := range compacted {
		if msg.Role == "assistant" && strings.Contains(msg.Content, "[Context compaction — layered summary]") {
			if !strings.Contains(msg.Content, "(brief)") {
				t.Fatalf("expected merged summary to contain '(brief)' section header")
			}
			if !strings.Contains(msg.Content, "(summary)") {
				t.Fatalf("expected merged summary to contain '(summary)' section header")
			}
			if !strings.Contains(msg.Content, "(detailed)") {
				t.Fatalf("expected merged summary to contain '(detailed)' section header")
			}
		}
	}

	// Verify anchor is preserved
	if compacted[0].Role != "system" {
		t.Fatalf("expected first message to be system, got %s", compacted[0].Role)
	}
	if compacted[1].Role != "user" {
		t.Fatalf("expected second message to be user anchor, got %s", compacted[1].Role)
	}

	// Verify recent messages are preserved intact (last few messages)
	lastMsg := compacted[len(compacted)-1]
	if lastMsg.Role != "assistant" {
		t.Fatalf("expected last message to be assistant, got %s", lastMsg.Role)
	}
}

func TestCompactConversationLayeredWithCheckpointSummaries(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	// Build messages where the middle segment contains checkpoint summaries
	// (assistant messages that look like checkpoint output)
	messages := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Fix all the bugs"},
		{Role: "assistant", Content: "I'll start by reading the failing tests."},
	}

	// Middle: 40 messages, including some that look like checkpoint summaries
	for i := 0; i < 10; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Continue fixing bug %d", i),
		})
		if i%3 == 0 {
			// Simulate a checkpoint summary already in the conversation
			messages = append(messages, api.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("Compacted earlier conversation state:\n- Summarized %d earlier messages.\n- Fixed bug %d", i*2, i),
			})
		} else {
			messages = append(messages, api.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("Fixed bug %d by updating the validation logic and adding proper error handling", i),
			})
		}
	}

	// More middle messages to get past the threshold
	for i := 0; i < 10; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Review fix %d", i),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Reviewed fix %d, looks good", i),
		})
	}

	// Recent: 24 messages
	for i := 0; i < 12; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Final check %d", i),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Final check %d done", i),
		})
	}

	compacted := optimizer.CompactConversation(messages)

	if len(compacted) >= len(messages) {
		t.Fatalf("expected compaction to reduce messages: got %d, original %d", len(compacted), len(messages))
	}

	// Verify compaction produced summaries
	summaryCount := 0
	for _, msg := range compacted {
		if msg.Role == "assistant" && strings.Contains(msg.Content, "Compacted earlier conversation state") {
			summaryCount++
		}
	}
	if summaryCount == 0 {
		t.Fatalf("expected at least 1 summary message, got 0")
	}
}

func TestIsCheckpointSummary(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	tests := []struct {
		content  string
		expected bool
	}{
		{"Compacted earlier conversation state:\n- Summarized 10 messages", true},
		{"User request: Fix the bug\nActions taken:\n- Read file.go", false},
		{"I've summarized the findings from the analysis", true}, // "summarized" triggers it
		{"The build succeeded and all tests pass", false},
		{"Status at compaction time: work in progress", true},
		{"", false},
	}

	for _, tc := range tests {
		result := optimizer.isCheckpointSummary(tc.content)
		if result != tc.expected {
			t.Errorf("isCheckpointSummary(%q) = %v, want %v", tc.content[:min(len(tc.content), 60)], result, tc.expected)
		}
	}
}

func TestMaskConsumedToolResults_Basic(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	largeContent := strings.Repeat("x", 4000)

	// Build messages with enough tool results to exceed observationMaskKeepLast (5).
	// We need 6+ tool results so at least 1 gets masked.
	messages := []api.Message{}
	for i := 0; i < 6; i++ {
		messages = append(messages, api.Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("call-%d", i)}},
		})
		messages = append(messages, api.Message{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call-%d", i),
			Content:    "Tool call result for read_file: big.go\n" + largeContent,
		})
	}
	// Final assistant message that consumes all tool results
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I've read the files and will proceed.",
	})

	optimized := optimizer.OptimizeConversation(messages)

	// The first tool result (index 1) should be masked (consumed, large, not in last N)
	if len(optimized) != len(messages) {
		t.Fatalf("expected same message count, got %d -> %d", len(messages), len(optimized))
	}

	if !strings.Contains(optimized[1].Content, "[PREVIOUS RESULT:") {
		t.Errorf("expected first tool result to be masked, got: %s", optimized[1].Content)
	}

	// Verify placeholder contains tool name
	if !strings.Contains(optimized[1].Content, "read_file") {
		t.Errorf("expected placeholder to contain tool name 'read_file', got: %s", optimized[1].Content)
	}

	// Verify the assistant response at end is unchanged
	if optimized[len(optimized)-1].Content != "I've read the files and will proceed." {
		t.Errorf("expected assistant message unchanged, got: %s", optimized[len(optimized)-1].Content)
	}
}

func TestMaskConsumedToolResults_SmallResultNotMasked(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	smallContent := "short result"

	messages := []api.Message{
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "call-1"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "Tool call result for read_file: small.go\n" + smallContent},
		{Role: "assistant", Content: "I've read the file."},
	}

	optimized := optimizer.OptimizeConversation(messages)

	// The tool result should NOT be masked (too small)
	if len(optimized) != len(messages) {
		t.Fatalf("expected same message count, got %d -> %d", len(messages), len(optimized))
	}

	if strings.Contains(optimized[1].Content, "[PREVIOUS RESULT:") {
		t.Errorf("expected small tool result NOT to be masked, got: %s", optimized[1].Content)
	}

	// Content should be preserved
	if !strings.Contains(optimized[1].Content, smallContent) {
		t.Errorf("expected small content to be preserved, got: %s", optimized[1].Content)
	}
}

func TestMaskConsumedToolResults_LastNKept(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	largeContent := strings.Repeat("x", 4000)

	// Build messages with many tool results followed by an assistant message.
	// Layout: assistant(tool_call) + tool_result x 10, then assistant(response)
	messages := []api.Message{}
	for i := 0; i < 10; i++ {
		messages = append(messages, api.Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("call-%d", i)}},
		})
		messages = append(messages, api.Message{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call-%d", i),
			Content:    "Tool call result for read_file: file" + fmt.Sprintf("%d", i) + ".go\n" + largeContent,
		})
	}
	// Final assistant message that consumes all tool results
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I've processed all the files.",
	})

	optimized := optimizer.OptimizeConversation(messages)

	// Count how many tool results are masked vs unmasked
	maskedCount := 0
	unmaskedCount := 0
	for i, msg := range optimized {
		if msg.Role == "tool" {
			if strings.Contains(msg.Content, "[PREVIOUS RESULT:") {
				maskedCount++
			} else {
				unmaskedCount++
			}
			// Verify that unmasked tool results preserve original content
			if unmaskedCount > 0 && !strings.Contains(msg.Content, largeContent) {
				t.Errorf("unmasked tool result at index %d should preserve original content", i)
			}
		}
	}

	// We have 10 tool results, keep last 5 unmasked → at most 5 should be masked
	// (some may not be masked if they're small, but all are 4000+ chars so all eligible)
	if unmaskedCount < observationMaskKeepLast {
		t.Errorf("expected at least %d unmasked tool results, got %d", observationMaskKeepLast, unmaskedCount)
	}

	if maskedCount == 0 {
		t.Error("expected some tool results to be masked, got 0")
	}

	t.Logf("masked: %d, unmasked: %d", maskedCount, unmaskedCount)
}

func TestMaskConsumedToolResults_NoAssistantAfter(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	largeContent := strings.Repeat("x", 4000)

	// Tool result with no assistant message after it (model hasn't seen it yet)
	messages := []api.Message{
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "call-1"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "Tool call result for read_file: big.go\n" + largeContent},
	}

	optimized := optimizer.OptimizeConversation(messages)

	// The tool result should NOT be masked (no assistant after it = not consumed)
	if len(optimized) != len(messages) {
		t.Fatalf("expected same message count, got %d -> %d", len(messages), len(optimized))
	}

	if strings.Contains(optimized[1].Content, "[PREVIOUS RESULT:") {
		t.Errorf("expected tool result without assistant after NOT to be masked, got: %s", optimized[1].Content)
	}

	// Content should be preserved
	if !strings.Contains(optimized[1].Content, largeContent) {
		t.Errorf("expected content to be preserved, got: %s", optimized[1].Content)
	}
}

func TestMaskConsumedToolResults_PlaceholderFormat(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	largeContent := strings.Repeat("x", 4000)

	// Build 6 tool results so the first one gets masked (exceeds keep-last-5)
	messages := []api.Message{}
	for i := 0; i < 6; i++ {
		messages = append(messages, api.Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("call-%d", i)}},
		})
		messages = append(messages, api.Message{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call-%d", i),
			Content:    "Tool call result for read_file: big.go\n" + largeContent,
		})
	}
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I've read the files and will proceed.",
	})

	optimized := optimizer.OptimizeConversation(messages)

	placeholder := optimized[1].Content

	// Verify format: [PREVIOUS RESULT: <tool_name>, <N> chars, <N> lines]
	if !strings.HasPrefix(placeholder, "[PREVIOUS RESULT:") {
		t.Errorf("expected placeholder to start with '[PREVIOUS RESULT:', got: %s", placeholder)
	}
	if !strings.HasSuffix(placeholder, " lines]") {
		t.Errorf("expected placeholder to end with ' lines]', got: %s", placeholder)
	}
	if !strings.Contains(placeholder, "read_file") {
		t.Errorf("expected placeholder to contain tool name 'read_file', got: %s", placeholder)
	}
	if !strings.Contains(placeholder, " chars,") {
		t.Errorf("expected placeholder to contain char count, got: %s", placeholder)
	}

	// Verify the placeholder is much shorter than the original
	originalLen := len(messages[1].Content)
	placeholderLen := len(placeholder)
	if placeholderLen >= originalLen {
		t.Errorf("placeholder (%d chars) should be shorter than original (%d chars)", placeholderLen, originalLen)
	}

	t.Logf("original: %d chars, placeholder: %d chars — '%s'", originalLen, placeholderLen, placeholder)
}

func TestMaskConsumedToolResults_ToolCallIDFallback(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	largeContent := strings.Repeat("x", 4000)

	// Build 6 tool results without "Tool call result for" prefix so first one gets masked
	messages := []api.Message{}
	for i := 0; i < 6; i++ {
		callID := fmt.Sprintf("call-custom-%d", i)
		messages = append(messages, api.Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: []api.ToolCall{{ID: callID}},
		})
		messages = append(messages, api.Message{
			Role:       "tool",
			ToolCallID: callID,
			Content:    largeContent,
		})
	}
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I've processed the results.",
	})

	optimized := optimizer.OptimizeConversation(messages)

	// The first tool result should be masked with ToolCallID as the name
	if !strings.Contains(optimized[1].Content, "[PREVIOUS RESULT:") {
		t.Errorf("expected tool result to be masked, got: %s", optimized[1].Content)
	}

	// Should fall back to ToolCallID since no "Tool call result for" prefix
	if !strings.Contains(optimized[1].Content, "call-custom-0") {
		t.Errorf("expected placeholder to contain ToolCallID, got: %s", optimized[1].Content)
	}
}

func TestMaskConsumedToolResults_PreservesToolCallID(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	largeContent := strings.Repeat("x", 4000)

	// Build 6 tool results so the first one gets masked
	messages := []api.Message{}
	for i := 0; i < 6; i++ {
		callID := fmt.Sprintf("call-preserve-%d", i)
		messages = append(messages, api.Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: []api.ToolCall{{ID: callID}},
		})
		messages = append(messages, api.Message{
			Role:       "tool",
			ToolCallID: callID,
			Content:    "Tool call result for read_file: big.go\n" + largeContent,
		})
	}
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I've read the files.",
	})

	optimized := optimizer.OptimizeConversation(messages)

	// Verify ToolCallID is preserved after masking
	if optimized[1].ToolCallID != "call-preserve-0" {
		t.Errorf("expected ToolCallID to be preserved, got: %s", optimized[1].ToolCallID)
	}

	// Verify role is preserved
	if optimized[1].Role != "tool" {
		t.Errorf("expected role to be preserved as 'tool', got: %s", optimized[1].Role)
	}
}

func TestMaskConsumedToolResults_DedupThenMasking(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	largeContent := strings.Repeat("x", 4000)

	// Message layout:
	// 0: assistant (tool_call for read_file)
	// 1: tool (read_file big.go, 4000 chars) — first read
	// 2: assistant response
	// 3: assistant (tool_call for read_file)
	// 4: tool (read_file big.go, 4000 chars) — second read (same file, same content)
	// ... 4 more tool results to exceed observationMaskKeepLast ...
	// then final assistant
	messages := []api.Message{
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{{ID: "call-0"}}},
		{Role: "tool", ToolCallID: "call-0", Content: "Tool call result for read_file: big.go\n" + largeContent},
		{Role: "assistant", Content: "I read the file."},
	}
	// Add more tool results to exceed keep-last threshold
	for i := 1; i <= 6; i++ {
		messages = append(messages, api.Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("call-%d", i)}},
		})
		messages = append(messages, api.Message{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call-%d", i),
			Content:    "Tool call result for shell_command: echo " + fmt.Sprintf("%d", i) + "\n" + largeContent,
		})
	}
	// Final assistant
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "Done.",
	})

	optimized := optimizer.OptimizeConversation(messages)

	// The first file read (index 1) should be deduped to a short summary
	// (it's redundant because the same file was read earlier with same content).
	// But since message gap is only 3 messages (not >= 15), it won't be deduped.
	// However, it should still be masked because it's consumed and large.
	masked := 0
	for _, msg := range optimized {
		if msg.Role == "tool" && strings.Contains(msg.Content, "[PREVIOUS RESULT:") {
			masked++
		}
	}
	if masked == 0 {
		t.Error("expected some tool results to be masked")
	}

	// Verify no tool result has both [OPTIMIZED]/[STALE] AND [PREVIOUS RESULT:]
	for i, msg := range optimized {
		if msg.Role == "tool" {
			hasDedup := strings.Contains(msg.Content, "[OPTIMIZED]") || strings.Contains(msg.Content, "[STALE]")
			hasMask := strings.Contains(msg.Content, "[PREVIOUS RESULT:")
			if hasDedup && hasMask {
				t.Errorf("message at index %d has both dedup and masking markers — should only have one", i)
			}
		}
	}
}

func TestMaskConsumedToolResults_InterspersedUserMessages(t *testing.T) {
	optimizer := NewConversationOptimizer(true, false)

	largeContent := strings.Repeat("x", 4000)

	// Layout: tool results with user messages interspersed
	// user messages should not affect masking of tool results
	messages := []api.Message{}
	for i := 0; i < 8; i++ {
		messages = append(messages, api.Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("call-%d", i)}},
		})
		messages = append(messages, api.Message{
			Role:       "tool",
			ToolCallID: fmt.Sprintf("call-%d", i),
			Content:    "Tool call result for read_file: file" + fmt.Sprintf("%d", i) + ".go\n" + largeContent,
		})
		// Insert a user message after every other tool result
		if i%2 == 1 {
			messages = append(messages, api.Message{
				Role:    "user",
				Content: fmt.Sprintf("Follow-up question %d", i),
			})
		}
	}
	// Final assistant message
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I've processed everything.",
	})

	optimized := optimizer.OptimizeConversation(messages)

	// Verify: user messages should be preserved unchanged
	for _, msg := range optimized {
		if msg.Role == "user" {
			if !strings.HasPrefix(msg.Content, "Follow-up question") {
				t.Errorf("user message was modified: %s", msg.Content)
			}
			if strings.Contains(msg.Content, "[PREVIOUS RESULT:") {
				t.Errorf("user message should never be masked: %s", msg.Content)
			}
		}
	}

	// Verify: only tool-role messages are masked
	maskedCount := 0
	for _, msg := range optimized {
		if msg.Role == "tool" && strings.Contains(msg.Content, "[PREVIOUS RESULT:") {
			maskedCount++
		}
	}
	if maskedCount == 0 {
		t.Error("expected some tool results to be masked even with interspersed user messages")
	}
	t.Logf("masked %d tool results with interspersed user messages", maskedCount)
}
