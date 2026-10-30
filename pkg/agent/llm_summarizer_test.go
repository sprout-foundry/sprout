package agent

import (
	"context"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// The automatic compaction path must carry a manifest the model cannot get
// wrong. Artifact tracking is the dimension summarizers degrade on most, and
// a manifest read off the tool calls is derived rather than recalled.
func TestNewLLMSummarizer_AppendsFileChangeManifest(t *testing.T) {
	client := NewScriptedClient(NewScriptedResponseBuilder().Content("Refactored the parser.").Build())
	summarize := newLLMSummarizer(client, "test")
	if summarize == nil {
		t.Fatal("expected a summarizer for a non-nil client")
	}

	messages := []api.Message{
		{Role: "user", Content: "refactor the parser"},
		{Role: "assistant", ToolCalls: []api.ToolCall{{
			ID:       "tc1",
			Type:     "function",
			Function: api.ToolCallFunction{Name: "edit_file", Arguments: `{"path":"pkg/parser.go"}`},
		}}},
		{Role: "tool", Content: "ok", ToolCallID: "tc1"},
	}

	body, err := summarize(context.Background(), messages, core.SummarizerHint{})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if !strings.Contains(body, "Refactored the parser.") {
		t.Errorf("model prose missing from summary: %q", body)
	}
	if !strings.Contains(body, "Files modified during compacted segment:") {
		t.Errorf("file-change manifest missing from summary: %q", body)
	}
	if !strings.Contains(body, "pkg/parser.go") {
		t.Errorf("changed path missing from summary: %q", body)
	}
}

// A segment with no file writes adds nothing: the manifest is an artifact
// record, not boilerplate on every summary.
func TestNewLLMSummarizer_NoManifestWithoutFileChanges(t *testing.T) {
	client := NewScriptedClient(NewScriptedResponseBuilder().Content("Just talked.").Build())
	summarize := newLLMSummarizer(client, "test")

	body, err := summarize(context.Background(), []api.Message{
		{Role: "user", Content: "what does this do?"},
		{Role: "assistant", Content: "It parses things."},
	}, core.SummarizerHint{})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if strings.Contains(body, "Files modified during compacted segment:") {
		t.Errorf("expected no manifest without file writes, got: %q", body)
	}
}
