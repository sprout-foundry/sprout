package components

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureOutput captures stdout during test execution
func captureOutput(f func()) string {
	// Since the formatter writes directly to stdout, we need to test differently
	// For now, we'll test the internal methods and formatting logic
	return ""
}

func TestStreamingFormatter_NewStreamingFormatter(t *testing.T) {
	mu := &sync.Mutex{}
	sf := NewStreamingFormatter(mu)

	assert.NotNil(t, sf)
	assert.Equal(t, mu, sf.outputMutex)
	assert.True(t, sf.isFirstChunk)
	assert.Equal(t, 50*time.Millisecond, sf.minUpdateDelay)
	assert.Equal(t, 100, sf.maxBufferSize)
}

func TestStreamingFormatter_ApplyInlineFormatting(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	tests := []struct {
		name     string
		input    string
		contains []string // We'll check if output contains certain strings
	}{
		{
			name:     "inline code",
			input:    "This is `inline code` text",
			contains: []string{"inline code"},
		},
		{
			name:     "bold with asterisks",
			input:    "This is **bold text** here",
			contains: []string{"bold text"},
		},
		{
			name:     "bold with underscores",
			input:    "This is __bold text__ here",
			contains: []string{"bold text"},
		},
		{
			name:     "italic with asterisks",
			input:    "This is *italic text* here",
			contains: []string{"italic text"},
		},
		{
			name:     "italic with underscores",
			input:    "This is _italic text_ here",
			contains: []string{"italic text"},
		},
		{
			name:     "mixed formatting",
			input:    "This has **bold**, *italic*, and `code` mixed",
			contains: []string{"bold", "italic", "code"},
		},
		{
			name:     "nested italic in bold should not break",
			input:    "This is **bold with *italic* inside** text",
			contains: []string{"bold with", "italic", "inside"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that the method doesn't panic and produces output
			output := sf.applyInlineFormatting(tt.input)
			assert.NotEmpty(t, output)

			// Check that expected text is present
			for _, expected := range tt.contains {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestStreamingFormatter_BufferingLogic(t *testing.T) {
	mu := &sync.Mutex{}
	sf := NewStreamingFormatter(mu)

	// Test buffer accumulation
	sf.Write("Hello")
	assert.Equal(t, "Hello", sf.buffer.String())

	// Test that short content doesn't flush immediately
	sf.Write(" world")
	assert.Equal(t, "Hello world", sf.buffer.String())

	// Test that newline triggers flush
	sf.Write("\n")
	// After flush, buffer should be empty
	time.Sleep(10 * time.Millisecond) // Small delay to ensure flush
	assert.Equal(t, "", sf.buffer.String())
}

func TestStreamingFormatter_Reset(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	// Set some state
	sf.buffer.WriteString("test")
	sf.lineBuffer.WriteString("line")
	sf.isFirstChunk = false
	sf.lastWasNewline = true
	sf.inCodeBlock = true

	// Reset
	sf.Reset()

	// Verify all state is cleared
	assert.Equal(t, "", sf.buffer.String())
	assert.Equal(t, "", sf.lineBuffer.String())
	assert.True(t, sf.isFirstChunk)
	assert.False(t, sf.lastWasNewline)
	assert.False(t, sf.inCodeBlock)
}

func TestStreamingFormatter_CodeBlockDetection(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	// Simulate code block
	lines := []string{
		"Here is some code:",
		"```go",
		"func main() {",
		"    fmt.Println(\"Hello\")",
		"}",
		"```",
		"After code block",
	}

	for _, line := range lines {
		// The formatter should handle this without panicking
		sf.buffer.WriteString(line + "\n")
		sf.flush()
	}

	// Should have exited code block
	assert.False(t, sf.inCodeBlock)
}

func TestStreamingFormatter_MarkdownHeaders(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	headers := []string{
		"# Main Header",
		"## Sub Header",
		"### Level 3 Header",
		"#### Level 4 Header",
	}

	for _, header := range headers {
		// Test that headers are processed without errors
		sf.buffer.WriteString(header + "\n")
		sf.flush()
	}
}

func TestStreamingFormatter_Lists(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	lists := []string{
		"- Bullet item 1",
		"* Bullet item 2",
		"+ Bullet item 3",
		"1. Numbered item 1",
		"2. Numbered item 2",
	}

	for _, item := range lists {
		// Test that lists are processed without errors
		sf.buffer.WriteString(item + "\n")
		sf.flush()
	}
}

func TestStreamingFormatter_EdgeCases(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	edgeCases := []string{
		"",                  // Empty string
		"   ",               // Only spaces
		"---",               // Horizontal rule
		"***",               // Alt horizontal rule
		"> Blockquote",      // Blockquote
		"Regular text",      // Plain text
		"Text with * and _", // Special chars not forming markdown
		"**",                // Incomplete bold
		"``",                // Incomplete code
		"Multiple ** bold ** markers ** in ** text", // Multiple bold
	}

	for _, testCase := range edgeCases {
		// Ensure no panic occurs
		require.NotPanics(t, func() {
			sf.buffer.WriteString(testCase + "\n")
			sf.flush()
		})
	}
}

func TestStreamingFormatter_LargeBuffer(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	// Test that large buffer triggers flush
	largeText := strings.Repeat("a", 150) // Larger than maxBufferSize (100)

	sf.Write(largeText)

	// Buffer should have been flushed
	assert.Less(t, sf.buffer.Len(), 150)
}

func TestStreamingFormatter_Finalize(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	// Add some buffered content
	sf.buffer.WriteString("buffered")
	sf.lineBuffer.WriteString("line")

	// Finalize should flush everything
	sf.Finalize()

	assert.Equal(t, "", sf.buffer.String())
	assert.Equal(t, "", sf.lineBuffer.String())
}

func TestStreamingFormatter_ConcurrentAccess(t *testing.T) {
	mu := &sync.Mutex{}
	sf := NewStreamingFormatter(mu)

	// Test concurrent writes don't cause race conditions
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sf.Write(fmt.Sprintf("Line %d\n", n))
		}(i)
	}

	wg.Wait()
	sf.Finalize()
}

// Benchmark tests to ensure performance
func BenchmarkStreamingFormatter_ApplyInlineFormatting(b *testing.B) {
	sf := NewStreamingFormatter(nil)
	text := "This has **bold**, *italic*, `code`, and regular text mixed together"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sf.applyInlineFormatting(text)
	}
}

func BenchmarkStreamingFormatter_Write(b *testing.B) {
	sf := NewStreamingFormatter(nil)
	text := "This is a line of text that will be written\n"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sf.Write(text)
	}
}

// Test regex compilation at init time
func TestStreamingFormatter_RegexCompilation(t *testing.T) {
	// This test ensures all regex patterns compile without panic
	require.NotPanics(t, func() {
		sf := NewStreamingFormatter(nil)

		// Test all formatting patterns
		testInputs := []string{
			"**bold**",
			"__bold__",
			"*italic*",
			"_italic_",
			"`code`",
			"Mixed **bold** and *italic* and `code`",
		}

		for _, input := range testInputs {
			_ = sf.applyInlineFormatting(input)
		}
	})
}

// Test bold formatting with edge cases
func TestStreamingFormatter_BoldFormatting(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	testCases := []struct {
		name     string
		input    string
		contains []string // Strings that should be in the formatted output
	}{
		{
			name:     "simple bold",
			input:    "This is **bold text** here",
			contains: []string{"bold text"},
		},
		{
			name:     "bold with asterisk inside",
			input:    "This is **bold with * inside** text",
			contains: []string{"bold with * inside"},
		},
		{
			name:     "multiple bold",
			input:    "First **bold** and second **more bold** text",
			contains: []string{"bold", "more bold"},
		},
		{
			name:     "underscore bold",
			input:    "This is __underscore bold__ test",
			contains: []string{"underscore bold"},
		},
		{
			name:     "mixed formatting",
			input:    "**Bold** and *italic* and `code` together",
			contains: []string{"Bold", "italic", "code"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sf.applyInlineFormatting(tc.input)

			// Check that formatting was applied (output should be different from input)
			assert.NotEqual(t, tc.input, result, "Formatting should change the text")

			// Check that expected content is present
			for _, expected := range tc.contains {
				assert.Contains(t, result, expected, "Expected content should be present")
			}
		})
	}
}

// Helper function to test output contains ANSI color codes
func containsANSIColorCodes(text string) bool {
	return strings.Contains(text, "\033[") || strings.Contains(text, "\x1b[")
}

func TestStreamingFormatter_ColorOutput(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	// Test that formatting produces color codes
	tests := []struct {
		input    string
		hasColor bool
	}{
		{"**bold text**", true},
		{"*italic text*", true},
		{"`code block`", true},
		{"plain text", false},
	}

	for _, tt := range tests {
		output := sf.applyInlineFormatting(tt.input)
		if tt.hasColor {
			assert.True(t, containsANSIColorCodes(output),
				"Expected color codes in output for: %s", tt.input)
		}
	}
}
