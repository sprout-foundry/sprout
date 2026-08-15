package api

import (
	"encoding/json"
	"testing"
)

func TestRecoverLFM2ToolCalls_SingleCall(t *testing.T) {
	content := `<|tool_call_start|>[read_file(path='/some/file.go')]<|tool_call_end|>`
	calls, rest, ok := RecoverLFM2ToolCalls(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	tc := calls[0]
	if tc.Function.Name != "read_file" {
		t.Errorf("name=%s, want read_file", tc.Function.Name)
	}
	if tc.Type != "function" {
		t.Errorf("type=%s, want function", tc.Type)
	}
	if tc.ID == "" {
		t.Error("expected non-empty ID")
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("invalid arguments JSON: %v", err)
	}
	if args["path"] != "/some/file.go" {
		t.Errorf("path=%v, want /some/file.go", args["path"])
	}
	if rest != "" {
		t.Errorf("rest=%q, want empty", rest)
	}
}

func TestRecoverLFM2ToolCalls_MultipleCalls(t *testing.T) {
	content := `<|tool_call_start|>[read_file(path='/a.go')]<|tool_call_end|><|tool_call_start|>[read_file(path='/b.go')]<|tool_call_end|>`
	calls, rest, ok := RecoverLFM2ToolCalls(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	for i, tc := range calls {
		if tc.Function.Name != "read_file" {
			t.Errorf("call %d: name=%s, want read_file", i, tc.Function.Name)
		}
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		want := "/a.go"
		if i == 1 {
			want = "/b.go"
		}
		if args["path"] != want {
			t.Errorf("call %d: path=%v, want %s", i, args["path"], want)
		}
	}
	if rest != "" {
		t.Errorf("rest=%q, want empty", rest)
	}
}

func TestRecoverLFM2ToolCalls_WithLeadingText(t *testing.T) {
	content := `I'll read the file now.\n<|tool_call_start|>[read_file(path='/some/file.go')]<|tool_call_end|>`
	calls, rest, ok := RecoverLFM2ToolCalls(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Errorf("name=%s", calls[0].Function.Name)
	}
	if rest == "" {
		t.Error("expected non-empty rest (leading text)")
	}
}

func TestRecoverLFM2ToolCalls_MultipleArgs(t *testing.T) {
	content := `<|tool_call_start|>[write_file(path='/out.py', content='def f():\n    pass')]<|tool_call_end|>`
	calls, _, ok := RecoverLFM2ToolCalls(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	var args map[string]interface{}
	json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if args["path"] != "/out.py" {
		t.Errorf("path=%v", args["path"])
	}
	if args["content"] != "def f():\n    pass" {
		t.Errorf("content=%v", args["content"])
	}
}

func TestRecoverLFM2ToolCalls_NoMarkers(t *testing.T) {
	content := "Just a regular response with no tool calls."
	calls, rest, ok := RecoverLFM2ToolCalls(content)
	if ok {
		t.Fatal("expected ok=false")
	}
	if calls != nil {
		t.Fatal("expected nil calls")
	}
	if rest != content {
		t.Error("expected unchanged content")
	}
}

func TestRecoverLFM2ToolCalls_BooleanAndNone(t *testing.T) {
	content := `<|tool_call_start|>[shell_command(command='ls', background=False, timeout=None)]<|tool_call_end|>`
	calls, _, ok := RecoverLFM2ToolCalls(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	var args map[string]interface{}
	json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if args["background"] != false {
		t.Errorf("background=%v, want false", args["background"])
	}
	if args["timeout"] != nil {
		t.Errorf("timeout=%v, want nil", args["timeout"])
	}
	if args["command"] != "ls" {
		t.Errorf("command=%v", args["command"])
	}
}

func TestRecoverLFM2ToolCalls_NumberArgs(t *testing.T) {
	content := `<|tool_call_start|>[edit_file(view_range=[1, 50], path='/foo')]<|tool_call_end|>`
	calls, _, ok := RecoverLFM2ToolCalls(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if calls[0].Function.Name != "edit_file" {
		t.Errorf("name=%s", calls[0].Function.Name)
	}
}

func TestRecoverLFM2ToolCalls_SproutMixedFormat(t *testing.T) {
	// This is what we actually saw from the live test:
	// Multiple calls concatenated, some with background=False
	content := `<|tool_call_start|>[shell_command(command='ls -la pkg/gomlx/llm/gemma4/', background=False)]<|tool_call_end|>`
	calls, _, ok := RecoverLFM2ToolCalls(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "shell_command" {
		t.Errorf("name=%s", calls[0].Function.Name)
	}
	var args map[string]interface{}
	json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if args["command"] != "ls -la pkg/gomlx/llm/gemma4/" {
		t.Errorf("command=%v", args["command"])
	}
}
