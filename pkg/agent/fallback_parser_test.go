package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

type expectedToolCall struct {
	name string
	args map[string]interface{}
}

func TestFallbackParserParsesMultipleFormats(t *testing.T) {
	agent := &Agent{}
	parser := NewFallbackParser(agent)

	tests := []struct {
		name        string
		content     string
		wantCalls   []expectedToolCall
		wantCleaned string
	}{
		{
			name:    "json wrapper",
			content: `{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":{"file_path":"README.md"}}}]}`,
			wantCalls: []expectedToolCall{
				{
					name: "read_file",
					args: map[string]interface{}{"file_path": "README.md"},
				},
			},
			wantCleaned: "",
		},
		{
			name:    "markdown json block",
			content: "Sure, here's what I'll run:\n```json\n{\"name\":\"shell_command\",\"arguments\":{\"command\":\"ls\"}}\n```\nLet me know if you'd prefer another command.",
			wantCalls: []expectedToolCall{
				{
					name: "shell_command",
					args: map[string]interface{}{"command": "ls"},
				},
			},
			wantCleaned: "Sure, here's what I'll run:\nLet me know if you'd prefer another command.",
		},
		{
			name:    "xml style",
			content: "I'll execute now:\n<function=shell_command><parameter=command>ls -la</parameter></function>\nDone.",
			wantCalls: []expectedToolCall{
				{
					name: "shell_command",
					args: map[string]interface{}{"command": "ls -la"},
				},
			},
			wantCleaned: "I'll execute now:\nDone.",
		},
		{
			name:    "xml with trailing wrapper",
			content: "Queued command\n<function=shell_command><parameter=command>ls -la</parameter></function>\n</tool_call>\nThanks!",
			wantCalls: []expectedToolCall{
				{
					name: "shell_command",
					args: map[string]interface{}{"command": "ls -la"},
				},
			},
			wantCleaned: "Queued command\nThanks!",
		},
		{
			name:    "function style",
			content: "Attempting fallback\nname: read_file arguments: {\"file_path\":\"README.md\"}\nThanks.",
			wantCalls: []expectedToolCall{
				{
					name: "read_file",
					args: map[string]interface{}{"file_path": "README.md"},
				},
			},
			wantCleaned: "Attempting fallback\nThanks.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parser.Parse(tc.content)
			if result == nil {
				t.Fatalf("Parse returned nil for content: %q", tc.content)
			}

			if len(result.ToolCalls) != len(tc.wantCalls) {
				t.Fatalf("expected %d tool calls, got %d", len(tc.wantCalls), len(result.ToolCalls))
			}

			cleaned := strings.TrimSpace(result.CleanedContent)
			if cleaned != tc.wantCleaned {
				t.Fatalf("unexpected cleaned content. want %q got %q", tc.wantCleaned, cleaned)
			}

			for i, call := range result.ToolCalls {
				if call.Function.Name != tc.wantCalls[i].name {
					t.Fatalf("call %d: expected name %q, got %q", i, tc.wantCalls[i].name, call.Function.Name)
				}
				if call.ID == "" {
					t.Fatalf("call %d: expected non-empty id", i)
				}
				if call.Type != "function" {
					t.Fatalf("call %d: expected type 'function', got %q", i, call.Type)
				}

				var gotArgs map[string]interface{}
				if err := json.Unmarshal([]byte(call.Function.Arguments), &gotArgs); err != nil {
					t.Fatalf("call %d: failed to unmarshal arguments %q: %v", i, call.Function.Arguments, err)
				}

				if !equalJSONMaps(gotArgs, tc.wantCalls[i].args) {
					t.Fatalf("call %d: unexpected arguments. want %#v got %#v", i, tc.wantCalls[i].args, gotArgs)
				}
			}
		})
	}
}

func TestFallbackParserShouldUseFallback(t *testing.T) {
	agent := &Agent{}
	parser := NewFallbackParser(agent)

	if parser.ShouldUseFallback("no tools here", false) {
		t.Fatalf("expected fallback to be false when no patterns present")
	}

	if !parser.ShouldUseFallback("I'll call name: read_file arguments: {}", false) {
		t.Fatalf("expected fallback to be true when tool patterns present")
	}

	if parser.ShouldUseFallback("has tool_calls", true) {
		t.Fatalf("expected fallback to be false when structured tool calls already provided")
	}
}

func equalJSONMaps(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		switch av := v.(type) {
		case map[string]interface{}:
			bvMap, ok := bv.(map[string]interface{})
			if !ok {
				return false
			}
			if !equalJSONMaps(av, bvMap) {
				return false
			}
		default:
			if av != bv {
				return false
			}
		}
	}
	return true
}
