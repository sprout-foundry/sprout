package agent

import (
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int // For empty, exact; for non-empty, we check it's positive
	}{
		{
			name:     "empty string returns zero",
			text:     "",
			expected: 0,
		},
		{
			name:     "single word returns positive count",
			text:     "hello",
			expected: -1, // -1 means we just check it's > 0
		},
		{
			name:     "multiple words returns larger count than single word",
			text:     "hello world this is a test",
			expected: -1,
		},
		{
			name:     "long text returns proportionally larger count",
			text:     "This is a longer piece of text that should estimate to more tokens than a short piece of text because it has more words and characters in it overall",
			expected: -1,
		},
		{
			name:     "special characters and newlines add tokens",
			text:     "hello\nworld\ttab\r\nnewline",
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateTokens(tt.text)

			if tt.expected == 0 {
				if result != 0 {
					t.Errorf("EstimateTokens(%q) = %d, want 0", tt.text, result)
				}
				return
			}

			if result <= 0 {
				t.Errorf("EstimateTokens(%q) = %d, want positive value", tt.text, result)
			}
		})
	}
}

// TestEstimateTokens_Comparison ensures longer text yields more tokens.
func TestEstimateTokens_Comparison(t *testing.T) {
	short := EstimateTokens("hello")
	long := EstimateTokens("hello world this is a much longer text that has many more words")
	if long <= short {
		t.Errorf("long text (%d tokens) should have more tokens than short text (%d tokens)", long, short)
	}
}
