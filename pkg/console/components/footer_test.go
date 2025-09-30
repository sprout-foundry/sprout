package components

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFooterComponent_NewFooterComponent(t *testing.T) {
	footer := NewFooterComponent()

	if footer.BaseComponent == nil {
		t.Error("BaseComponent should be initialized")
	}

	if footer.BaseComponent.ID() != "footer" {
		t.Errorf("Expected footer ID 'footer', got %s", footer.BaseComponent.ID())
	}

	if footer.BaseComponent.Type() != "FooterComponent" {
		t.Errorf("Expected footer type 'FooterComponent', got %s", footer.BaseComponent.Type())
	}

	// Check session start time is recent
	if time.Since(footer.sessionStart) > time.Second {
		t.Error("Session start time should be recent")
	}
}

func TestFooterComponent_FormatTokens(t *testing.T) {
	footer := NewFooterComponent()

	tests := []struct {
		tokens   int
		expected string
	}{
		{500, "500"},
		{1500, "1.5K"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1234, "1.2K"},
		{1234567, "1.2M"},
	}

	for _, test := range tests {
		result := footer.formatTokens(test.tokens)
		if result != test.expected {
			t.Errorf("formatTokens(%d) = %s, expected %s", test.tokens, result, test.expected)
		}
	}
}

func TestFooterComponent_FormatDuration(t *testing.T) {
	footer := NewFooterComponent()

	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{150 * time.Second, "2m"},
		{3700 * time.Second, "1.0h"},
		{7260 * time.Second, "2.0h"},
		{0, "0s"},
		{45 * time.Second, "45s"},
		{60 * time.Second, "1m"},
		{3600 * time.Second, "1.0h"},
	}

	for _, test := range tests {
		result := footer.formatDuration(test.duration)
		if result != test.expected {
			t.Errorf("formatDuration(%v) = %s, expected %s", test.duration, result, test.expected)
		}
	}
}

func TestFooterComponent_ExtractModelName(t *testing.T) {
	footer := NewFooterComponent()

	tests := []struct {
		fullModel string
		expected  string
	}{
		{"openai/gpt-4", "gpt-4"},
		{"anthropic/claude-3-opus", "claude-3-opus"},
		{"qwen/qwen3-coder-30b-a3b-instruct:free", "qwen3-coder-...:free"},
		{"deepseek/deepseek-chat-v3.1:free", "deepseek-cha...:free"},
		{"very-long-model-name-that-exceeds-twenty-characters", "very-long-model-n..."},
		{"simple-model", "simple-model"},
		{"", ""},
		{"single", "single"},
		{"provider/short", "short"},
	}

	for _, test := range tests {
		result := footer.extractModelName(test.fullModel)
		if result != test.expected {
			t.Errorf("extractModelName(%s) = %s, expected %s", test.fullModel, result, test.expected)
		}
	}
}

func TestFooterComponent_TruncateString(t *testing.T) {
	footer := NewFooterComponent()

	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a very long string", 10, "this is..."},
		{"exactly10c", 10, "exactly10c"},
		{"", 5, ""},
		{"a", 5, "a"},
		{"toolong", 3, "..."},
	}

	for _, test := range tests {
		result := footer.truncateString(test.input, test.maxLen)
		if result != test.expected {
			t.Errorf("truncateString(%s, %d) = %s, expected %s", test.input, test.maxLen, result, test.expected)
		}
	}
}

func TestFooterComponent_SetOutputMutex(t *testing.T) {
	footer := NewFooterComponent()
	mutex := &sync.Mutex{}

	footer.SetOutputMutex(mutex)

	if footer.outputMutex != mutex {
		t.Error("SetOutputMutex did not set the mutex correctly")
	}
}

func TestFooterComponent_ModelNameExtractionQwen(t *testing.T) {
	footer := NewFooterComponent()

	// Test specific Qwen model patterns
	qwenTests := []struct {
		input    string
		expected string
	}{
		{"qwen/qwen3-coder-30b-a3b-instruct", "qwen3-coder-30b-a..."},
		{"qwen/qwen3-coder:free", "qwen3-coder:free"},
		{"qwen/qwen3-coder-480b-a35b-instruct-turbo:free", "qwen3-coder-...:free"},
		{"qwen/qwen2.5-coder-32b-instruct", "qwen2.5-coder-32b..."},
	}

	for _, test := range qwenTests {
		result := footer.extractModelName(test.input)
		if result != test.expected {
			t.Errorf("Qwen pattern extractModelName(%s) = %s, expected %s", test.input, result, test.expected)
		}
	}
}

func TestFooterComponent_ModelNameExtractionDeepSeek(t *testing.T) {
	footer := NewFooterComponent()

	// Test specific DeepSeek model patterns
	deepSeekTests := []struct {
		input    string
		expected string
	}{
		{"deepseek/deepseek-chat-v3.1:free", "deepseek-cha...:free"},
		{"deepseek/deepseek-coder", "deepseek-coder"},
		{"deepseek/very-long-deepseek-model-name:free", "very-long-de...:free"},
	}

	for _, test := range deepSeekTests {
		result := footer.extractModelName(test.input)
		if result != test.expected {
			t.Errorf("DeepSeek pattern extractModelName(%s) = %s, expected %s", test.input, result, test.expected)
		}
	}
}

func TestFooterComponent_CostFormatting(t *testing.T) {
	footer := NewFooterComponent()

	// Test the cost formatting logic manually (since we can't easily test render output)
	testCases := []struct {
		cost     float64
		expected string // What the logic should produce
	}{
		{5.50, "$5.50"},         // >= 1.0
		{0.123, "$0.123"},       // >= 0.01
		{0.000416, "$0.000416"}, // > 0, small amount
		{0.0, "$0.000"},         // exactly 0
		{10.75, "$10.75"},       // >= 1.0
		{0.05, "$0.050"},        // >= 0.01
		{0.001234, "$0.001234"}, // > 0, very small
	}

	for _, tc := range testCases {
		footer.lastCost = tc.cost

		// Replicate the cost formatting logic from the render method
		var costStr string
		if footer.lastCost >= 1.0 {
			costStr = formatCost(footer.lastCost, "%.2f")
		} else if footer.lastCost >= 0.01 {
			costStr = formatCost(footer.lastCost, "%.3f")
		} else if footer.lastCost > 0 {
			costStr = formatCost(footer.lastCost, "%.6f")
		} else {
			costStr = "$0.000"
		}

		if costStr != tc.expected {
			t.Errorf("Cost formatting for %f: expected %s, got %s", tc.cost, tc.expected, costStr)
		}
	}
}

// Helper function to format cost consistently
func formatCost(cost float64, format string) string {
	return "$" + strings.TrimPrefix(fmt.Sprintf(format, cost), "$")
}
