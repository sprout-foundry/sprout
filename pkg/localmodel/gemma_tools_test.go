//go:build darwin && arm64 && cgo

package localmodel

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Round-trip: format a tool call natively, parse it back.
func TestGemmaToolCallRoundTrip(t *testing.T) {
	raw := `<|tool_call>call:read_file{path:<|"|>/tmp/notes.txt<|"|>}<tool_call|>`
	content, calls := parseGemmaToolCalls(raw)
	if content != "" {
		t.Errorf("content = %q, want empty", content)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Errorf("name = %q", calls[0].Function.Name)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args not JSON: %v", err)
	}
	if args["path"] != "/tmp/notes.txt" {
		t.Errorf("path = %v", args["path"])
	}
}

// Multiple calls in one emission parse independently.
func TestGemmaMultipleCalls(t *testing.T) {
	raw := `<|tool_call>call:list_dir{path:<|"|>/tmp<|"|>}<tool_call|><|tool_call>call:read_file{path:<|"|>/tmp/a.txt<|"|>}<tool_call|>`
	_, calls := parseGemmaToolCalls(raw)
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if calls[0].Function.Name != "list_dir" || calls[1].Function.Name != "read_file" {
		t.Errorf("names = %q, %q", calls[0].Function.Name, calls[1].Function.Name)
	}
}

// The hallucinated-opener shape observed live: <|tool_response> where
// <|tool_call> belongs, with a well-formed call body after it.
func TestGemmaHallucinatedOpener(t *testing.T) {
	raw := `<|tool_response>call:read_file{path:<|"|>/tmp/notes.txt<|"|>}<tool_call|>`
	_, calls := parseGemmaToolCalls(raw)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Errorf("name = %q", calls[0].Function.Name)
	}
}

// Typed values survive: numbers, booleans, nested objects.
func TestGemmaTypedArgs(t *testing.T) {
	raw := `<|tool_call>call:write_file{path:<|"|>/tmp/x<|"|>,overwrite:false,max:3} <tool_call|>`
	_, calls := parseGemmaToolCalls(raw)
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if args["overwrite"] != false {
		t.Errorf("overwrite = %v (%T), want false", args["overwrite"], args["overwrite"])
	}
	if args["max"] != float64(3) {
		t.Errorf("max = %v (%T), want 3", args["max"], args["max"])
	}
}

// Thought-channel spans are stripped from content.
func TestGemmaStripThinking(t *testing.T) {
	in := "<|channel>thought\nI should list first.\n<channel|>Let me look." +
		"<|tool_call>call:list_dir{path:<|\"|>/tmp<|\"|>}<tool_call|>"
	content, calls := parseGemmaToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	if strings.Contains(content, "thought") || strings.Contains(content, "<|channel>") {
		t.Errorf("thinking leaked into content: %q", content)
	}
	if content != "Let me look." {
		t.Errorf("content = %q", content)
	}
}

// formatGemmaToolResponse shape matches the canonical template.
func TestGemmaToolResponseFormat(t *testing.T) {
	got := gemmaFormatToolResponse("read_file", "line1\nline2")
	want := `<|tool_response>response:read_file{value:<|"|>line1` + "\n" + `line2<|"|>}<tool_response|>`
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

// formatGemmaAssistantToolCalls renders replayable native calls.
func TestGemmaAssistantReplayFormat(t *testing.T) {
	calls := []api.ToolCall{{
		ID:       "1",
		Type:     "function",
		Function: api.ToolCallFunction{Name: "list_dir", Arguments: `{"path":"/tmp"}`},
	}}
	got := gemmaFormatAssistantToolCalls(calls)
	want := `<|tool_call>call:list_dir{path:<|"|>/tmp<|"|>}<tool_call|>`
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

// Declarations render in canonical format.
func TestGemmaDeclarations(t *testing.T) {
	tools := []api.Tool{{
		Type: "function",
		Function: api.ToolFunction{
			Name:        "read_file",
			Description: "Read a file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "abs"},
				},
				"required": []string{"path"},
			},
		},
	}}
	got := gemmaToolDeclarations(tools)
	want := `<|tool>declaration:read_file{description:<|"|>Read a file<|"|>,parameters:{properties:{path:{description:<|"|>abs<|"|>,type:<|"|>STRING<|"|>}},required:[<|"|>path<|"|>],type:<|"|>OBJECT<|"|>}}<tool|>`
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}
