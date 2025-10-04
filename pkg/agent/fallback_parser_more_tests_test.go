package agent

import (
    "testing"
)

// Malformed JSON inside a code block should not panic and should return nil (no tool calls).
func TestFallbackParser_MalformedJSON_NoPanic(t *testing.T) {
    agent := &Agent{debug: true}
    parser := NewFallbackParser(agent)
    content := "Here is a block:\n```json\n{\"name\":\"shell_command\", \"arguments\": {\"command\": \"ls\"} // trailing garbage\n```\nDone"

    result := parser.Parse(content)
    if result != nil && len(result.ToolCalls) > 0 {
        t.Fatalf("expected no tool calls for malformed JSON, got %d", len(result.ToolCalls))
    }
}

// Mixed valid/invalid array entries: parser should return only the valid tool calls.
func TestFallbackParser_MixedValidInvalidArray(t *testing.T) {
    agent := &Agent{}
    parser := NewFallbackParser(agent)
    content := `[
        {"type":"function","function":{"name":"read_file","arguments":{"file_path":"README.md"}}},
        {"type":"function","function":{"name":123,"arguments":{}}},
        {"type":"function","function":{"name":"shell_command","arguments":{"command":"echo hi"}}}
    ]`

    result := parser.Parse(content)
    if result == nil {
        t.Fatalf("expected result, got nil")
    }
    if len(result.ToolCalls) != 2 {
        t.Fatalf("expected 2 valid calls, got %d", len(result.ToolCalls))
    }
}

