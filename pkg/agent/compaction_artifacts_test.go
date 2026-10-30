package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// The record seed appends to a compaction summary is derived from the tool
// calls, so it cannot be wrong about which files changed.
func TestCompactionArtifactBlock_RendersFileChanges(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "refactor the parser"},
		{Role: "assistant", ToolCalls: []api.ToolCall{{
			ID:       "tc1",
			Type:     "function",
			Function: api.ToolCallFunction{Name: "edit_file", Arguments: `{"path":"pkg/parser.go"}`},
		}}},
		{Role: "tool", Content: "ok", ToolCallID: "tc1"},
	}

	block := CompactionArtifactBlock(msgs)
	if !strings.Contains(block, "pkg/parser.go") {
		t.Errorf("expected the changed path in the artifact block, got %q", block)
	}
}

// A segment with no file writes contributes nothing; the record is an
// artifact log, not boilerplate on every summary.
func TestCompactionArtifactBlock_EmptyWithoutFileChanges(t *testing.T) {
	block := CompactionArtifactBlock([]api.Message{
		{Role: "user", Content: "what does this do?"},
		{Role: "assistant", Content: "It parses things."},
	})
	if block != "" {
		t.Errorf("expected no artifact block without file changes, got %q", block)
	}
}

// A record written into an earlier summary is read back out, so a file edited
// before a compaction stays listed after later ones.
func TestCompactionArtifactBlock_CarriesForwardPriorRecord(t *testing.T) {
	msgs := []api.Message{
		{Role: "assistant", Content: "Compacted earlier conversation state:\n- did some work\n\nFiles modified during compacted segment:\n- M pkg/parser.go"},
		{Role: "user", Content: "now update the lexer"},
		{Role: "assistant", ToolCalls: []api.ToolCall{{
			ID:       "tc2",
			Type:     "function",
			Function: api.ToolCallFunction{Name: "write_file", Arguments: `{"path":"pkg/lexer.go"}`},
		}}},
		{Role: "tool", Content: "ok", ToolCallID: "tc2"},
	}

	block := CompactionArtifactBlock(msgs)
	for _, want := range []string{"pkg/parser.go", "pkg/lexer.go"} {
		if !strings.Contains(block, want) {
			t.Errorf("expected %q in the carried-forward record, got %q", want, block)
		}
	}
}
