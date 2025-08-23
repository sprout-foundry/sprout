package llm

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/prompts"
)

func TestShouldUseJSONResponse(t *testing.T) {
	tests := []struct {
		name     string
		messages []prompts.Message
		expected bool
	}{
		{
			name: "JSON required in system message",
			messages: []prompts.Message{
				{Role: "system", Content: "You must respond with a valid JSON object only"},
			},
			expected: true,
		},
		{
			name: "JSON format only",
			messages: []prompts.Message{
				{Role: "system", Content: "Return only JSON format"},
			},
			expected: true,
		},
		{
			name: "No JSON requirement",
			messages: []prompts.Message{
				{Role: "system", Content: "Please provide a helpful response"},
			},
			expected: false,
		},
		{
			name: "User message with JSON requirement",
			messages: []prompts.Message{
				{Role: "user", Content: "Respond with JSON only"},
			},
			expected: true,
		},
		{
			name: "Multiple messages with JSON in system",
			messages: []prompts.Message{
				{Role: "system", Content: "You must return only a valid JSON object"},
				{Role: "user", Content: "Hello"},
			},
			expected: true,
		},
		{
			name: "Non-string content",
			messages: []prompts.Message{
				{Role: "system", Content: 123}, // Non-string content
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUseJSONResponse(tt.messages)
			if result != tt.expected {
				t.Errorf("ShouldUseJSONResponse() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
