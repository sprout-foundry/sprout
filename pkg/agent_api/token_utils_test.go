package api

import (
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		minItems int
		maxItems int
	}{
		{
			name:     "empty string",
			input:    "",
			minItems: 0,
			maxItems: 0,
		},
		{
			name:     "simple text",
			input:    "Hello world",
			minItems: 2,
			maxItems: 5,
		},
		{
			name:     "code content",
			input:    "func main() {\n\treturn 42\n}",
			minItems: 5,
			maxItems: 20,
		},
		{
			name:     "text with newlines",
			input:    "Line 1\nLine 2\nLine 3",
			minItems: 3,
			maxItems: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateTokens(tt.input)
			if result < tt.minItems {
				t.Errorf("EstimateTokens(%q) = %d, want at least %d", tt.input, result, tt.minItems)
			}
			if result > tt.maxItems {
				t.Errorf("EstimateTokens(%q) = %d, want at most %d", tt.input, result, tt.maxItems)
			}
		})
	}
}

func TestEstimateInputTokens(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		tools    []Tool
		min      int
	}{
		{
			name:     "empty messages and tools",
			messages: nil,
			tools:    nil,
			min:      SystemInstructionBuffer, // At minimum the system buffer
		},
		{
			name: "single message",
			messages: []Message{
				{Role: "user", Content: "Hello world"},
			},
			tools: nil,
			min:   SystemInstructionBuffer + MessageOverheadTokens, // buffer + message overhead
		},
		{
			name: "message with tools",
			messages: []Message{
				{Role: "user", Content: "Hello"},
			},
			tools: []Tool{
				{Type: "function", Function: struct {
					Name        string      `json:"name"`
					Description string      `json:"description"`
					Parameters  interface{} `json:"parameters"`
				}{Name: "test"}},
				{Type: "function", Function: struct {
					Name        string      `json:"name"`
					Description string      `json:"description"`
					Parameters  interface{} `json:"parameters"`
				}{Name: "test2"}},
			},
			min: SystemInstructionBuffer + MessageOverheadTokens + (2 * ToolTokenEstimate),
		},
		{
			name: "message with reasoning content",
			messages: []Message{
				{
					Role:             "assistant",
					Content:          "Short answer",
					ReasoningContent: "Longer hidden reasoning content that must count toward prompt tokens",
				},
			},
			tools: nil,
			min:   SystemInstructionBuffer + MessageOverheadTokens + EstimateTokens("Longer hidden reasoning content that must count toward prompt tokens"),
		},
		{
			name: "message with image payload",
			messages: []Message{
				{
					Role:    "user",
					Content: "Describe this image",
					Images: []ImageData{
						{
							URL:  "https://example.com/cat.png",
							Type: "image/png",
						},
					},
				},
			},
			tools: nil,
			min:   SystemInstructionBuffer + MessageOverheadTokens + ImageMessageOverheadTokens,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateInputTokens(tt.messages, tt.tools)
			if result < tt.min {
				t.Errorf("EstimateInputTokens() = %d, want at least %d", result, tt.min)
			}
		})
	}
}

func TestCalculateOutputBudget(t *testing.T) {
	tests := []struct {
		name         string
		contextLimit int
		inputTokens  int
		wantOK       bool
		minOutput    int
		maxOutput    int
	}{
		{
			name:         "normal case",
			contextLimit: 32000,
			inputTokens:  10000,
			wantOK:       true,
			minOutput:    MinOutputTokens,
			maxOutput:    21000, // 32K - 10K = 22K, minus ~5% buffer (~1.1K) = ~20.9K
		},
		{
			name:         "input exceeds context",
			contextLimit: 4000,
			inputTokens:  5000,
			wantOK:       false,
			minOutput:    0,
			maxOutput:    0,
		},
		{
			name:         "input equals context",
			contextLimit: 4000,
			inputTokens:  4000,
			wantOK:       false,
			minOutput:    0,
			maxOutput:    0,
		},
		{
			name:         "small context minimum output",
			contextLimit: 2000,
			inputTokens:  500,
			wantOK:       true,
			minOutput:    MinOutputTokens, // Should clamp to minimum
			maxOutput:    1500,
		},
		{
			name:         "budget never exceeds remaining context",
			contextLimit: 1200,
			inputTokens:  900,
			wantOK:       true,
			minOutput:    300,
			maxOutput:    300,
		},
		{
			name:         "zero context limit uses default",
			contextLimit: 0,
			inputTokens:  1000,
			wantOK:       true,
			minOutput:    MinOutputTokens,
			maxOutput:    30000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := CalculateOutputBudget(tt.contextLimit, tt.inputTokens)
			if ok != tt.wantOK {
				t.Errorf("CalculateOutputBudget() ok = %v, want %v", ok, tt.wantOK)
			}
			if result < tt.minOutput {
				t.Errorf("CalculateOutputBudget() = %d, want at least %d", result, tt.minOutput)
			}
			if tt.maxOutput > 0 && result > tt.maxOutput {
				t.Errorf("CalculateOutputBudget() = %d, want at most %d", result, tt.maxOutput)
			}
		})
	}
}

func TestDetectCode(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"func main() {}", true},
		{"package main\n\nimport \"fmt\"", true},
		{"Hello world", false},
		{"This is plain text with no code", false},
		{"if x > 0 {", true},
		{"return nil", true},
		{"const x = 1", true},
		{"var y int", true},
		{"struct Foo {}", true},
		{"interface Bar {}", true},
		{"func() {", true},
		{"=> {", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := detectCode(tt.input)
			if result != tt.expected {
				t.Errorf("detectCode(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
