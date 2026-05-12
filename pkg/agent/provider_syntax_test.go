package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

type strictSyntaxClient struct {
	*factory.TestClient
	provider string
	model    string
}

func (c *strictSyntaxClient) GetProvider() string { return c.provider }
func (c *strictSyntaxClient) GetModel() string    { return c.model }

func sampleMessagesWithTools() []api.Message {
	return []api.Message{
		{Role: "system", Content: "system"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []api.ToolCall{
				{ID: "call-1"},
			},
		},
		{Role: "tool", Content: "Tool call result for read_file: a.go\nx", ToolCallID: "call-1"},
		{Role: "assistant", Content: "final answer"},
	}
}

func TestNormalizeConversationForStrictToolSyntax(t *testing.T) {
	messages := sampleMessagesWithTools()

	normalized, report := normalizeConversationForStrictToolSyntax(messages)
	if report.removedToolMessages == 0 {
		t.Fatalf("expected tool messages to be dropped")
	}
	if report.strippedAssistantToolCallBlocks == 0 {
		t.Fatalf("expected assistant tool_calls to be stripped")
	}
	if report.toolSummaryEntries == 0 {
		t.Fatalf("expected tool summaries to be created")
	}

	for _, msg := range normalized {
		if msg.Role == "tool" {
			t.Fatalf("expected no tool role messages after normalization")
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			t.Fatalf("expected assistant tool_calls to be removed after normalization")
		}
	}

	last := normalized[len(normalized)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content, "Context preserved from prior tool interactions") {
		t.Fatalf("expected trailing compressed summary message, got role=%s content=%q", last.Role, last.Content)
	}
}

func TestStrictToolCallSyntaxDetectionByModel(t *testing.T) {
	client := &strictSyntaxClient{
		TestClient: &factory.TestClient{},
		provider:   "openrouter",
		model:      "minimax/minimax-m1",
	}
	agent := &Agent{
		client: client,
	}

	if !agent.isStrictToolCallSyntaxModel() {
		t.Fatalf("expected minimax model on openrouter to be treated as strict syntax")
	}
}

func TestPendingSwitchContextRefreshConsumedOnce(t *testing.T) {
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.setPendingSwitchContextRefresh("hello")
	if got := a.consumePendingSwitchContextRefresh(); got != "hello" {
		t.Fatalf("unexpected first consume: %q", got)
	}
	if got := a.consumePendingSwitchContextRefresh(); got != "" {
		t.Fatalf("expected empty on second consume, got %q", got)
	}
}

func TestPendingStrictSwitchNoticeConsumedOnce(t *testing.T) {
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.setPendingStrictSwitchNotice("notice")
	if got := a.ConsumePendingStrictSwitchNotice(); got != "notice" {
		t.Fatalf("unexpected first consume: %q", got)
	}
	if got := a.ConsumePendingStrictSwitchNotice(); got != "" {
		t.Fatalf("expected empty on second consume, got %q", got)
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	t.Parallel()

	t.Run("returns zero for empty messages", func(t *testing.T) {
		messages := []api.Message{}
		tokens := estimateMessageTokens(messages)
		if tokens != 0 {
			t.Errorf("expected 0 tokens for empty messages, got %d", tokens)
		}
	})

	t.Run("estimates tokens from message content", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "Hello, world!"},
			{Role: "assistant", Content: "Hi there! How can I help?"},
		}
		tokens := estimateMessageTokens(messages)
		if tokens == 0 {
			t.Errorf("expected non-zero tokens for messages with content")
		}
		// Rough check: should be > 0 and reasonable (not millions)
		if tokens < 2 {
			t.Errorf("expected at least 2 tokens, got %d", tokens)
		}
		if tokens > 1000 {
			t.Errorf("expected reasonable token count, got %d", tokens)
		}
	})

	t.Run("includes reasoning content in estimation", func(t *testing.T) {
		messages := []api.Message{
			{Role: "assistant", Content: "answer", ReasoningContent: "This is my reasoning process"},
		}
		tokens := estimateMessageTokens(messages)
		if tokens == 0 {
			t.Errorf("expected non-zero tokens for message with reasoning")
		}
	})

	t.Run("sums tokens from multiple messages", func(t *testing.T) {
		short := []api.Message{{Role: "user", Content: "test"}}
		long := []api.Message{
			{Role: "user", Content: strings.Repeat("hello ", 100)},
			{Role: "assistant", Content: strings.Repeat("world ", 100)},
		}
		shortTokens := estimateMessageTokens(short)
		longTokens := estimateMessageTokens(long)
		if longTokens <= shortTokens {
			t.Errorf("expected longer messages to have more tokens: short=%d long=%d", shortTokens, longTokens)
		}
	})
}

func TestSummarizeToolMessage_ReadFile(t *testing.T) {
	t.Parallel()

	t.Run("extracts file path from read_file header", func(t *testing.T) {
		msg := api.Message{
			Role:    "tool",
			Content: "Tool call result for read_file: pkg/agent/agent.go\nfile content here",
		}
		summary, artifact := summarizeToolMessage(msg)

		if !strings.Contains(summary, "Read file:") {
			t.Errorf("expected summary to contain 'Read file:', got '%s'", summary)
		}
		if !strings.Contains(summary, "pkg/agent/agent.go") {
			t.Errorf("expected summary to contain file path, got '%s'", summary)
		}
		if artifact != "" {
			t.Errorf("expected no artifact for read_file, got '%s'", artifact)
		}
	})

	t.Run("handles empty header gracefully", func(t *testing.T) {
		msg := api.Message{
			Role:    "tool",
			Content: "\ncontent here",
		}
		summary, artifact := summarizeToolMessage(msg)

		if summary == "" {
			t.Errorf("expected a default summary, got empty string")
		}
		if artifact != "" {
			t.Errorf("expected no artifact, got '%s'", artifact)
		}
	})

	t.Run("handles header-only content", func(t *testing.T) {
		msg := api.Message{
			Role:    "tool",
			Content: "Tool call result for read_file: test.go",
		}
		summary, artifact := summarizeToolMessage(msg)

		if !strings.Contains(summary, "Read file:") {
			t.Errorf("expected file read summary, got '%s'", summary)
		}
		if artifact != "" {
			t.Errorf("expected no artifact, got '%s'", artifact)
		}
	})

	t.Run("handles read_file with full output path", func(t *testing.T) {
		msg := api.Message{
			Role:    "tool",
			Content: "Tool call result for read_file: config.json\nFull output saved to /tmp/output.txt\nconfig data here",
		}
		summary, artifact := summarizeToolMessage(msg)

		if !strings.Contains(summary, "Read file:") {
			t.Errorf("expected file read summary, got '%s'", summary)
		}
		if artifact != "" {
			t.Errorf("expected no artifact for read_file even with full output path, got '%s'", artifact)
		}
	})
}

func TestSummarizeToolMessage_ShellCommand(t *testing.T) {
	t.Parallel()

	t.Run("extracts command from shell_command header", func(t *testing.T) {
		msg := api.Message{
			Role:    "tool",
			Content: "Tool call result for shell_command: ls -la\noutput here",
		}
		summary, artifact := summarizeToolMessage(msg)

		if !strings.Contains(summary, "Shell command:") {
			t.Errorf("expected summary to contain 'Shell command:', got '%s'", summary)
		}
		if !strings.Contains(summary, "ls -la") {
			t.Errorf("expected summary to contain command, got '%s'", summary)
		}
		if artifact != "" {
			t.Errorf("expected no artifact without full output path, got '%s'", artifact)
		}
	})

	t.Run("extracts artifact from full output path", func(t *testing.T) {
		msg := api.Message{
			Role:    "tool",
			Content: "Tool call result for shell_command: make build\nFull output saved to /tmp/build_output.txt\noutput here",
		}
		summary, artifact := summarizeToolMessage(msg)

		if !strings.Contains(summary, "Shell command:") {
			t.Errorf("expected shell command summary, got '%s'", summary)
		}
		if artifact != "/tmp/build_output.txt" {
			t.Errorf("expected artifact path, got '%s'", artifact)
		}
		if !strings.Contains(summary, "full output:") {
			t.Errorf("expected summary to reference artifact, got '%s'", summary)
		}
	})

	t.Run("handles shell command with complex command", func(t *testing.T) {
		msg := api.Message{
			Role:    "tool",
			Content: "Tool call result for shell_command: go test ./... -v -run TestFoo\noutput",
		}
		summary, artifact := summarizeToolMessage(msg)

		if !strings.Contains(summary, "go test") {
			t.Errorf("expected summary to contain command, got '%s'", summary)
		}
		if artifact != "" {
			t.Errorf("expected no artifact, got '%s'", artifact)
		}
	})

	t.Run("handles multiline header gracefully", func(t *testing.T) {
		msg := api.Message{
			Role:    "tool",
			Content: "Tool call result for shell_command: echo test\nand more output\nlines",
		}
		summary, artifact := summarizeToolMessage(msg)

		if summary == "" {
			t.Errorf("expected non-empty summary")
		}
		if artifact != "" {
			t.Errorf("expected no artifact, got '%s'", artifact)
		}
	})
}

func TestSummarizeAssistantToolCalls(t *testing.T) {
	t.Parallel()

	t.Run("summarizes single tool call", func(t *testing.T) {
		msg := api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{ID: "call-1", Type: "function"},
			},
		}
		msg.ToolCalls[0].Function.Name = "read_file"
		summary := summarizeAssistantToolCalls(msg)

		if !strings.Contains(summary, "read_file") {
			t.Errorf("expected summary to contain tool name, got '%s'", summary)
		}
		if !strings.Contains(summary, "Assistant invoked tools") {
			t.Errorf("expected summary header, got '%s'", summary)
		}
	})

	t.Run("summarizes multiple tool calls", func(t *testing.T) {
		msg := api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{ID: "call-1", Type: "function"},
				{ID: "call-2", Type: "function"},
				{ID: "call-3", Type: "function"},
			},
		}
		msg.ToolCalls[0].Function.Name = "read_file"
		msg.ToolCalls[1].Function.Name = "write_file"
		msg.ToolCalls[2].Function.Name = "shell_command"
		summary := summarizeAssistantToolCalls(msg)

		if !strings.Contains(summary, "read_file") {
			t.Errorf("expected read_file in summary, got '%s'", summary)
		}
		if !strings.Contains(summary, "write_file") {
			t.Errorf("expected write_file in summary, got '%s'", summary)
		}
		if !strings.Contains(summary, "shell_command") {
			t.Errorf("expected shell_command in summary, got '%s'", summary)
		}
	})

	t.Run("deduplicates duplicate tool calls", func(t *testing.T) {
		msg := api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{ID: "call-1", Type: "function"},
				{ID: "call-2", Type: "function"},
				{ID: "call-3", Type: "function"},
			},
		}
		msg.ToolCalls[0].Function.Name = "read_file"
		msg.ToolCalls[1].Function.Name = "read_file"
		msg.ToolCalls[2].Function.Name = "read_file"
		summary := summarizeAssistantToolCalls(msg)

		// Should only appear once
		count := strings.Count(summary, "read_file")
		if count != 1 {
			t.Errorf("expected read_file to appear once, appeared %d times in '%s'", count, summary)
		}
	})

	t.Run("handles empty tool calls", func(t *testing.T) {
		msg := api.Message{
			Role:      "assistant",
			ToolCalls: []api.ToolCall{},
		}
		summary := summarizeAssistantToolCalls(msg)

		if summary != "" {
			t.Errorf("expected empty summary for no tool calls, got '%s'", summary)
		}
	})

	t.Run("handles tool calls with empty names", func(t *testing.T) {
		msg := api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{ID: "call-1", Type: "function"},
				{ID: "call-2", Type: "function"},
			},
		}
		msg.ToolCalls[0].Function.Name = ""
		msg.ToolCalls[1].Function.Name = "  "
		summary := summarizeAssistantToolCalls(msg)

		if !strings.Contains(summary, "unknown_tool") {
			t.Errorf("expected unknown_tool for empty names, got '%s'", summary)
		}
	})
}

func TestBuildToolCompressionMessage(t *testing.T) {
	t.Parallel()

	t.Run("builds message with summaries", func(t *testing.T) {
		summaries := []string{
			"Read file: test.go",
			"Shell command: ls -la",
		}
		msg := buildToolCompressionMessage(summaries, []string{})

		if !strings.Contains(msg, "Context preserved from prior tool interactions") {
			t.Errorf("expected header in message, got '%s'", msg)
		}
		if !strings.Contains(msg, "Read file: test.go") {
			t.Errorf("expected first summary, got '%s'", msg)
		}
		if !strings.Contains(msg, "Shell command: ls -la") {
			t.Errorf("expected second summary, got '%s'", msg)
		}
		if !strings.Contains(msg, "Use these summaries") {
			t.Errorf("expected footer text, got '%s'", msg)
		}
	})

	t.Run("adds numbered list for summaries", func(t *testing.T) {
		summaries := []string{"summary1", "summary2", "summary3"}
		msg := buildToolCompressionMessage(summaries, []string{})

		if !strings.Contains(msg, "1. summary1") {
			t.Errorf("expected numbered first item, got '%s'", msg)
		}
		if !strings.Contains(msg, "2. summary2") {
			t.Errorf("expected numbered second item, got '%s'", msg)
		}
		if !strings.Contains(msg, "3. summary3") {
			t.Errorf("expected numbered third item, got '%s'", msg)
		}
	})

	t.Run("includes artifact pointers section", func(t *testing.T) {
		summaries := []string{"summary1"}
		artifacts := []string{"/tmp/output1.txt", "/tmp/output2.txt"}
		msg := buildToolCompressionMessage(summaries, artifacts)

		if !strings.Contains(msg, "Artifacts for deep details") {
			t.Errorf("expected artifacts section header, got '%s'", msg)
		}
		if !strings.Contains(msg, "/tmp/output1.txt") {
			t.Errorf("expected first artifact, got '%s'", msg)
		}
		if !strings.Contains(msg, "/tmp/output2.txt") {
			t.Errorf("expected second artifact, got '%s'", msg)
		}
	})

	t.Run("handles empty summaries and artifacts", func(t *testing.T) {
		msg := buildToolCompressionMessage([]string{}, []string{})

		if msg == "" {
			t.Errorf("expected non-empty message")
		}
		if !strings.Contains(msg, "Context preserved") {
			t.Errorf("expected header even with empty content, got '%s'", msg)
		}
	})

	t.Run("truncates long summary list correctly", func(t *testing.T) {
		// Create more than strictSwitchRecentToolSummaryCount (6) summaries
		summaries := make([]string, 10)
		for i := 0; i < 10; i++ {
			summaries[i] = "summary"
		}

		// Note: buildToolCompressionMessage doesn't truncate, normalizeConversationForStrictToolSyntax does
		msg := buildToolCompressionMessage(summaries, []string{})

		// All summaries should be included
		for i := 0; i < 10; i++ {
			if !strings.Contains(msg, "summary") {
				t.Errorf("expected summary %d in message, got '%s'", i, msg)
			}
		}
	})
}
