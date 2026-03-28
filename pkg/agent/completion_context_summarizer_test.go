package agent

import (
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestCompletionContextSummarizerKeepsFullConversationWhenContextHasHeadroom(t *testing.T) {
	summarizer := NewCompletionContextSummarizer(false)
	messages := []api.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "First request"},
		{Role: "assistant", Content: strings.Repeat("Detailed implementation notes. ", 20)},
		{Role: "user", Content: "Follow-up request"},
	}

	if summarizer.ShouldApplySummarization(messages, 1000, 100000, "openrouter", true) {
		t.Fatalf("did not expect completion summarization while context has ample headroom")
	}

	out := summarizer.ApplyCompletionSummarization(messages, 1000, 100000, "openrouter", true)
	if len(out) != len(messages) {
		t.Fatalf("expected full conversation to remain intact, got %d messages instead of %d", len(out), len(messages))
	}
}

func TestCompletionContextSummarizerUsesSamePruningGateWhenContextIsTight(t *testing.T) {
	summarizer := NewCompletionContextSummarizer(false)
	messages := []api.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "First request"},
		{Role: "assistant", Content: strings.Repeat("Detailed implementation notes. ", 20)},
		{Role: "user", Content: "Follow-up request"},
	}

	if !summarizer.ShouldApplySummarization(messages, 98000, 100000, "openrouter", true) {
		t.Fatalf("expected completion summarization once the normal pruning gate is crossed")
	}

	out := summarizer.ApplyCompletionSummarization(messages, 98000, 100000, "openrouter", true)
	if len(out) >= len(messages) {
		t.Fatalf("expected summarized conversation to shrink message count, got %d -> %d", len(messages), len(out))
	}
}
