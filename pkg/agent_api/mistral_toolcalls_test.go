package api

import (
	"encoding/json"
	"testing"
)

func TestRecoverMistralToolCalls(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantOK    bool
		wantName  string // first call's name
		wantArgs  string // first call's arguments (JSON string)
		wantCount int
		wantRest  string
	}{
		{
			name:      "name+object shape",
			content:   `[TOOL_CALLS]submit_todos{"summary": "x", "todos": "1. fix"}`,
			wantOK:    true,
			wantName:  "submit_todos",
			wantArgs:  `{"summary": "x", "todos": "1. fix"}`,
			wantCount: 1,
		},
		{
			name:      "json array shape with object arguments",
			content:   `[TOOL_CALLS][{"name": "read_file", "arguments": {"path": "config.json"}}]`,
			wantOK:    true,
			wantName:  "read_file",
			wantArgs:  `{"path": "config.json"}`,
			wantCount: 1,
		},
		{
			name:      "array with double-encoded string arguments",
			content:   `[TOOL_CALLS][{"name": "f", "arguments": "{\"a\":1}"}]`,
			wantOK:    true,
			wantName:  "f",
			wantArgs:  `{"a":1}`,
			wantCount: 1,
		},
		{
			name:      "multiple concatenated name+object calls",
			content:   `[TOOL_CALLS]a{"x":1}b{"y":2}`,
			wantOK:    true,
			wantName:  "a",
			wantCount: 2,
		},
		{
			name:     "leading prose preserved as remaining content",
			content:  "Let me do that.\n[TOOL_CALLS]list_dir{\"path\": \".\"}",
			wantOK:   true,
			wantName: "list_dir",
			wantRest: "Let me do that.",
		},
		{name: "no marker", content: "just a normal answer", wantOK: false},
		{name: "marker but empty payload", content: "[TOOL_CALLS]   ", wantOK: false},
		{name: "marker but garbage payload", content: "[TOOL_CALLS]not a tool call at all", wantOK: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls, rest, ok := RecoverMistralToolCalls(c.content)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v (calls=%+v)", ok, c.wantOK, calls)
			}
			if !ok {
				if rest != c.content {
					t.Errorf("on failure, content must be returned unchanged: got %q", rest)
				}
				return
			}
			if len(calls) != c.wantCount && c.wantCount != 0 {
				t.Errorf("count = %d, want %d", len(calls), c.wantCount)
			}
			if c.wantName != "" && calls[0].Function.Name != c.wantName {
				t.Errorf("name = %q, want %q", calls[0].Function.Name, c.wantName)
			}
			if c.wantArgs != "" && calls[0].Function.Arguments != c.wantArgs {
				t.Errorf("args = %q, want %q", calls[0].Function.Arguments, c.wantArgs)
			}
			if c.wantRest != "" && rest != c.wantRest {
				t.Errorf("remaining = %q, want %q", rest, c.wantRest)
			}
			// Recovered calls must have an ID/type and JSON-parseable arguments.
			for i, call := range calls {
				if call.ID == "" || call.Type != "function" {
					t.Errorf("call %d missing ID/type: %+v", i, call)
				}
				var m map[string]any
				if json.Unmarshal([]byte(call.Function.Arguments), &m) != nil {
					t.Errorf("call %d arguments not valid JSON object: %q", i, call.Function.Arguments)
				}
			}
		})
	}
}
