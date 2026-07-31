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

func TestEstimateInputTokensIncludesToolMetadata(t *testing.T) {
	t.Run("assistant tool_calls increase estimate", func(t *testing.T) {
		base := []Message{
			{Role: "assistant", Content: "Use the calculator."},
		}
		withToolCalls := []Message{
			{
				Role:    "assistant",
				Content: "Use the calculator.",
				ToolCalls: []ToolCall{
					{
						ID:   "call_1",
						Type: "function",
					},
				},
			},
		}
		withToolCalls[0].ToolCalls[0].Function.Name = "calculator"
		withToolCalls[0].ToolCalls[0].Function.Arguments = `{"value":1}`

		baseTokens := EstimateInputTokens(base, nil)
		toolTokens := EstimateInputTokens(withToolCalls, nil)
		if toolTokens <= baseTokens {
			t.Fatalf("expected tool_calls to increase estimate, base=%d tool_calls=%d", baseTokens, toolTokens)
		}
		minExpectedDelta := EstimateTokens("call_1") + EstimateTokens("function") + EstimateTokens("calculator") + EstimateTokens(`{"value":1}`) + ToolCallOverheadTokens
		if toolTokens-baseTokens < minExpectedDelta {
			t.Fatalf("expected tool_calls delta >= %d, got %d", minExpectedDelta, toolTokens-baseTokens)
		}
	})

	t.Run("tool_call_id increase estimate", func(t *testing.T) {
		base := []Message{
			{Role: "tool", Content: "42"},
		}
		withToolCallID := []Message{
			{Role: "tool", Content: "42", ToolCallID: "call_1"},
		}

		baseTokens := EstimateInputTokens(base, nil)
		toolTokens := EstimateInputTokens(withToolCallID, nil)
		if toolTokens <= baseTokens {
			t.Fatalf("expected tool_call_id to increase estimate, base=%d tool_call_id=%d", baseTokens, toolTokens)
		}
		minExpectedDelta := EstimateTokens("call_1") + ToolCallIDOverheadTokens
		if toolTokens-baseTokens < minExpectedDelta {
			t.Fatalf("expected tool_call_id delta >= %d, got %d", minExpectedDelta, toolTokens-baseTokens)
		}
	})
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
			// worstCaseInput=10000+3000=13000, cushion=max(2000,1600)=2000, output=32000-13000-2000=17000
			maxOutput: 17000,
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
			minOutput:    MinOutputTokens, // buffer = 20% of 2000 = 400, floored to 4000 >= remaining (1500), returns min
			maxOutput:    MinOutputTokens,
		},
		{
			name:         "budget never exceeds remaining context",
			contextLimit: 1200,
			inputTokens:  900,
			wantOK:       true,
			minOutput:    300, // buffer = 20% of 1200 = 240, floored to 4000 >= remaining (300), returns remaining
			maxOutput:    300,
		},
		{
			name:         "zero context limit uses default",
			contextLimit: 0,
			inputTokens:  1000,
			wantOK:       true,
			minOutput:    MinOutputTokens,
			// defaults to 32K, worstCaseInput=1000+300=1300, cushion=max(2000,1600)=2000, output=32000-1300-2000=28700
			maxOutput: 28700,
		},
		{
			// Regression test for the original "context window exceeded" error
			// (see git history: estimated 116145 tokens, actual 156146 — a
			// 34.4% underestimate — caused input+output to total 200001 with
			// the old flat 20%-of-context buffer). The worst-case-input model
			// must still absorb that real gap: 116000*1.3=150800 worst-case,
			// +10000 cushion = 160800 threshold, comfortably above the actual
			// 156146 that was observed.
			name:         "large context with heavy input - estimation gap absorbed",
			contextLimit: 200000,
			inputTokens:  116000,
			wantOK:       true,
			minOutput:    MinOutputTokens,
			// worstCaseInput=116000+34800=150800, cushion=max(2000,10000)=10000, output=200000-150800-10000=39200
			maxOutput: 39200,
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

// TestCalculateOutputBudgetNoPrematureCollapse is a regression test for a bug
// where CalculateOutputBudget stacked two additive buffers (20% of context +
// 25% of input). Once estimated input crossed ~64% of a 200K context, the
// combined buffer mathematically exceeded the remaining space and the
// function fell through to a hard-coded 512-token floor — tens of thousands
// of tokens before the real ceiling. Every model response was then truncated
// at 512 tokens mid-reasoning/mid-tool-call for the rest of the conversation.
//
// The fix inflates the input estimate to a worst-case figure instead of
// stacking buffers, so the output budget tapers gracefully and the floor is
// only reached in the final stretch before the actual ceiling.
func TestCalculateOutputBudgetNoPrematureCollapse(t *testing.T) {
	contextLimit := 200000
	tests := []struct {
		name        string
		inputTokens int
		minOutput   int // must stay comfortably above the old 512-token floor
	}{
		{name: "50% of context", inputTokens: 100000, minOutput: 20000},
		{name: "60% of context", inputTokens: 120000, minOutput: 10000},
		{name: "64% of context - old collapse point", inputTokens: 128000, minOutput: 5000},
		{name: "70% of context", inputTokens: 140000, minOutput: 2000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := CalculateOutputBudget(contextLimit, tt.inputTokens)
			if !ok {
				t.Fatalf("CalculateOutputBudget(%d, %d) not ok", contextLimit, tt.inputTokens)
			}
			if result < tt.minOutput {
				t.Errorf("CalculateOutputBudget(%d, %d) = %d, want at least %d (premature collapse to floor)",
					contextLimit, tt.inputTokens, result, tt.minOutput)
			}
			if result <= MinOutputTokens {
				t.Errorf("CalculateOutputBudget(%d, %d) = %d, collapsed to/below the emergency floor (%d) far from the real ceiling",
					contextLimit, tt.inputTokens, result, MinOutputTokens)
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
