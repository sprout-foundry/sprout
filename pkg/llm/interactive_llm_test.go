package llm

import (
	"testing"
)

func TestContainsToolCallWithActualResponse(t *testing.T) {
	// Test the exact response format from the terminal output
	response := `{"tool_calls": [{"id": "call_7", "type": "function", "function": {"name": "edit_file_section", "arguments": {"file_path": "pkg/llm/providers.go", "old_text": "var providerEndpoints = map[string]string{\n\t\"openai\":    \"https://api.openai.com/v1\",\n\t\"anthropic\": \"https://api.anthropic.com/v1\",\n\t\"google\":    \"https://generativelanguage.googleapis.com/v1beta\",\n}\n\n\tendpoint, ok := providerEndpoints[provider]\n\tif !ok {\n\n\treturn endpoint, nil", "new_text": "package llm\n\nimport \"fmt\"\n\nvar providerEndpoints = map[string]string{\n\t\"openai\":    \"https://api.openai.com/v1\",\n\t\"anthropic\": \"https://api.anthropic.com/v1\",\n\t\"google\":    \"https://generativelanguage.googleapis.com/v1beta\",\n}\n\nfunc GetProviderEndpoint(provider string) (string, error) {\n\tendpoint, ok := providerEndpoints[provider]\n\tif !ok {\n\t\treturn \"\", fmt.Errorf(\"unknown provider: %s\", provider)\n\t}\n\treturn endpoint, nil\n}"}}]}`

	// Test containsToolCall
	if !containsToolCall(response) {
		t.Errorf("containsToolCall should return true for this response")
		t.Logf("Response starts with {: %v", response[0] == '{')
		t.Logf("Contains tool_calls: %v", containsToolCall(response))
	} else {
		t.Log("✅ containsToolCall correctly detected tool_calls")
	}

	// Test parseToolCalls
	toolCalls, err := parseToolCalls(response)
	if err != nil {
		t.Errorf("parseToolCalls failed: %v", err)
		return
	}

	if len(toolCalls) == 0 {
		t.Errorf("parseToolCalls should return at least one tool call")
	} else {
		t.Logf("✅ parseToolCalls found %d tool calls", len(toolCalls))
		for i, tc := range toolCalls {
			t.Logf("Tool call %d: ID=%s, Type=%s, Function=%s", i+1, tc.ID, tc.Type, tc.Function.Name)
		}
	}
}

func TestContainsToolCallEdgeCases(t *testing.T) {
	testCases := []struct {
		name     string
		response string
		expected bool
	}{
		{
			name:     "simple tool call",
			response: `{"tool_calls": [{"id": "1", "type": "function", "function": {"name": "test", "arguments": "{}"}}]}`,
			expected: true,
		},
		{
			name:     "tool call with newlines",
			response: "{\n  \"tool_calls\": [\n    {\n      \"id\": \"1\",\n      \"type\": \"function\",\n      \"function\": {\n        \"name\": \"test\",\n        \"arguments\": \"{}\"\n      }\n    }\n  ]\n}",
			expected: true,
		},
		{
			name:     "no tool calls",
			response: `{"message": "hello world"}`,
			expected: false,
		},
		{
			name:     "tool call in JSON code block",
			response: "```json\n{\"tool_calls\": [{\"id\": \"1\", \"type\": \"function\", \"function\": {\"name\": \"test\", \"arguments\": \"{}\"}}]}\n```",
			expected: true,
		},
		{
			name:     "empty response",
			response: "",
			expected: false,
		},
		{
			name:     "response without opening brace",
			response: `"tool_calls": [{"id": "1"}]`,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := containsToolCall(tc.response)
			if result != tc.expected {
				t.Errorf("containsToolCall(%q) = %v, expected %v", tc.response, result, tc.expected)
			}
		})
	}
}
