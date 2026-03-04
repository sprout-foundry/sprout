package agent

import (
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
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
		{Role: "tool", Content: "Tool call result for read_file: a.go\nx", ToolCallId: "call-1"},
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
	a := &Agent{}
	a.setPendingSwitchContextRefresh("hello")
	if got := a.consumePendingSwitchContextRefresh(); got != "hello" {
		t.Fatalf("unexpected first consume: %q", got)
	}
	if got := a.consumePendingSwitchContextRefresh(); got != "" {
		t.Fatalf("expected empty on second consume, got %q", got)
	}
}

func TestPendingStrictSwitchNoticeConsumedOnce(t *testing.T) {
	a := &Agent{}
	a.setPendingStrictSwitchNotice("notice")
	if got := a.ConsumePendingStrictSwitchNotice(); got != "notice" {
		t.Fatalf("unexpected first consume: %q", got)
	}
	if got := a.ConsumePendingStrictSwitchNotice(); got != "" {
		t.Fatalf("expected empty on second consume, got %q", got)
	}
}
