package ui

import (
	"strings"
	"testing"
)

func TestModelItem_DisplayCompact(t *testing.T) {
	tests := []struct {
		name             string
		modelItem        *ModelItem
		maxWidth         int
		expectedPrefix   string
		expectedContains []string
	}{
		{
			name: "Model with input/output pricing and context",
			modelItem: &ModelItem{
				Provider:      "openai",
				Model:         "gpt-5",
				InputCost:     0.0025,
				OutputCost:    0.01,
				ContextLength: 128000,
			},
			maxWidth:         40,
			expectedPrefix:   "gpt-5",
			expectedContains: []string{"$0.003/$0.010/M", "128K"},
		},
		{
			name: "Model with legacy pricing",
			modelItem: &ModelItem{
				Provider:      "anthropic",
				Model:         "claude-3-sonnet",
				LegacyCost:    0.003,
				ContextLength: 200000,
			},
			maxWidth:         50,
			expectedPrefix:   "claude-3-sonnet",
			expectedContains: []string{"$0.003/M", "200K"},
		},
		{
			name: "Free Ollama model",
			modelItem: &ModelItem{
				Provider:      "Ollama (Local)",
				Model:         "llama3",
				ContextLength: 4096,
			},
			maxWidth:         30,
			expectedPrefix:   "llama3",
			expectedContains: []string{"FREE", "4K"},
		},
		{
			name: "Model without pricing info",
			modelItem: &ModelItem{
				Provider: "huggingface",
				Model:    "codellama",
			},
			maxWidth:         20,
			expectedPrefix:   "codellama",
			expectedContains: []string{},
		},
		{
			name: "Very narrow width prioritizes model name",
			modelItem: &ModelItem{
				Provider:      "openai",
				Model:         "gpt-4-turbo",
				InputCost:     0.001,
				OutputCost:    0.002,
				ContextLength: 128000,
			},
			maxWidth:         15,
			expectedPrefix:   "gpt-4-turbo",
			expectedContains: []string{}, // Should truncate before pricing info
		},
		{
			name: "Medium width shows pricing but not context",
			modelItem: &ModelItem{
				Provider:      "openai",
				Model:         "gpt-4",
				InputCost:     0.03,
				OutputCost:    0.06,
				ContextLength: 8192,
			},
			maxWidth:         25,
			expectedPrefix:   "gpt-4",
			expectedContains: []string{"$0.030/$0.060/M"}, // Should show pricing but not context
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.modelItem.DisplayCompact(tt.maxWidth)

			// Should start with model name
			if !strings.HasPrefix(result, tt.expectedPrefix) {
				t.Errorf("DisplayCompact() result %q should start with %q", result, tt.expectedPrefix)
			}

			// Should contain expected pricing/context info
			for _, expected := range tt.expectedContains {
				if !strings.Contains(result, expected) {
					t.Errorf("DisplayCompact() result %q should contain %q", result, expected)
				}
			}

			// Should not exceed max width
			if len(result) > tt.maxWidth {
				t.Errorf("DisplayCompact() result %q exceeds max width %d (length: %d)", result, tt.maxWidth, len(result))
			}
		})
	}
}

func TestModelItem_DisplayCompact_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		modelItem *ModelItem
		maxWidth  int
		expect    string
	}{
		{
			name: "Zero max width",
			modelItem: &ModelItem{
				Provider: "test",
				Model:    "model",
			},
			maxWidth: 0,
			expect:   "",
		},
		{
			name: "Negative max width",
			modelItem: &ModelItem{
				Provider: "test",
				Model:    "model",
			},
			maxWidth: -10,
			expect:   "",
		},
		{
			name: "Very small max width",
			modelItem: &ModelItem{
				Provider: "test",
				Model:    "x",
			},
			maxWidth: 1,
			expect:   "x", // Uses model name
		},
		{
			name: "Exact width match",
			modelItem: &ModelItem{
				Provider: "test",
				Model:    "abc",
			},
			maxWidth: 3,
			expect:   "abc", // Uses model name
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.modelItem.DisplayCompact(tt.maxWidth)
			if result != tt.expect {
				t.Errorf("DisplayCompact(%d) = %q, want %q", tt.maxWidth, result, tt.expect)
			}
		})
	}
}

func TestModelItem_DisplayCompact_WithDisplayName(t *testing.T) {
	modelItem := &ModelItem{
		Provider:      "openai",
		Model:         "gpt-5",
		DisplayName:   "GPT-5",
		InputCost:     0.0025,
		OutputCost:    0.01,
		ContextLength: 128000,
	}

	result := modelItem.DisplayCompact(40)

	// Should use DisplayName instead of Model
	if !strings.HasPrefix(result, "GPT-4 Omni") {
		t.Errorf("DisplayCompact() should use DisplayName. Got %q", result)
	}

	// Should still contain pricing and context info
	if !strings.Contains(result, "$0.003/$0.010/M") || !strings.Contains(result, "128K") {
		t.Errorf("DisplayCompact() should contain pricing and context info. Got %q", result)
	}
}

func BenchmarkModelItem_DisplayCompact(b *testing.B) {
	modelItem := &ModelItem{
		Provider:      "openai",
		Model:         "gpt-5",
		InputCost:     0.0025,
		OutputCost:    0.01,
		ContextLength: 128000,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		modelItem.DisplayCompact(50)
	}
}
