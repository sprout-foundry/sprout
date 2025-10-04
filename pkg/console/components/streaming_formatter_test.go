package components

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	outputMutex := &sync.Mutex{}
	sf := NewStreamingFormatter(outputMutex)

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
	// Use nil mutex to disable output during test
	sf := NewStreamingFormatter(nil)

	// Test basic writing functionality
	sf.Write("Hello")
	// Note: The new streaming implementation may flush content more aggressively
	// for better user experience, so we test that content is processed rather
	// than specifically retained in the buffer

	sf.Write(" world")

	// Test that newline triggers flush
	sf.Write("\n")
	// After flush, buffer should be empty
	time.Sleep(10 * time.Millisecond) // Small delay to ensure flush
	assert.Equal(t, "", sf.buffer.String())

	// Test that the formatter is working (doesn't crash)
	assert.NotNil(t, sf)
	assert.False(t, sf.finalized)
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

func TestStreamingFormatter_ForceFlushEmitsPendingCodeFence(t *testing.T) {
	sf := NewStreamingFormatter(nil)

	callCount := 0
	var captured []string
	sf.SetOutputFunc(func(text string) {
		callCount++
		captured = append(captured, text)
	})

	// Sanity-check that the output function is wired correctly.
	sf.println("direct test")
	require.Equal(t, 1, callCount)
	callCount = 0
	captured = captured[:0]

	// Simulate a streamed code block where the closing fence arrives without a trailing newline.
	sf.Write("```go\n")
	t.Logf("after opening fence: calls=%d captured=%d buffer=%d lineBuffer=%d", callCount, len(captured), sf.buffer.Len(), sf.lineBuffer.Len())
	require.Equal(t, 0, sf.buffer.Len(), "opening fence should flush immediately")
	sf.Write("fmt.Println(\"hi\")\n")
	t.Logf("after code line: calls=%d captured=%d buffer=%d lineBuffer=%d", callCount, len(captured), sf.buffer.Len(), sf.lineBuffer.Len())
	require.Equal(t, 0, sf.buffer.Len(), "code line should flush immediately")
	sf.Write("```")
	t.Logf("after closing fence chunk: calls=%d captured=%d buffer=%d lineBuffer=%d", callCount, len(captured), sf.buffer.Len(), sf.lineBuffer.Len())

	// The closing fence should still be buffered at this point and no additional output produced.
	require.Greater(t, len(captured), 0, "expected prior writes to produce visible output")
	before := callCount
	require.Greater(t, sf.buffer.Len()+sf.lineBuffer.Len(), 0, "pending fence should be buffered before force flush")
	require.True(t, sf.inCodeBlock, "should still be in code block before flush")

	// Force flush should emit the buffered fence and exit the code block state.
	sf.ForceFlush()

	require.Greater(t, callCount, before, "force flush should emit buffered fence content")
	require.Equal(t, 0, sf.buffer.Len(), "buffer should be empty after flush")
	require.Equal(t, 0, sf.lineBuffer.Len(), "line buffer should be cleared after flush")
	require.False(t, sf.inCodeBlock, "code block flag should be reset after closing fence")
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

	// Force color output for testing
	color.NoColor = false

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

	// Reset color behavior
	color.NoColor = true
}

func TestStreamingFormatter_XMLToolCallFiltering(t *testing.T) {
	mu := &sync.Mutex{}
	sf := NewStreamingFormatter(mu)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:  "simple XML tool call",
			input: `<function=shell_command><parameter=command>ls -la</parameter></function>`,
			expected: `🔧 shell_command
`,
		},
		{
			name:  "XML tool call with text before and after",
			input: `Here's the output: <function=shell_command><parameter=command>ls</parameter></function> Done!`,
			expected: `Here's the output: 🔧 shell_command
 Done!`,
		},
		{
			name: "multiple XML tool calls",
			input: `<function=shell_command><parameter=command>ls</parameter></function>

<function=read_file><parameter=file_path>test.txt</parameter></function>`,
			expected: `🔧 shell_command

🔧 read_file
`,
		},
		{
			name:  "XML tool call with tool_call closing tag",
			input: `<function=shell_command><parameter=command>ls</parameter></tool_call>`,
			expected: `🔧 shell_command
`,
		},
		{
			name: "mixed content with XML tool calls",
			input: `Let me check the files:

<function=shell_command>
<parameter=command>
ls -la
</parameter>
</function>

Now let me read a file:

<function=read_file>
<parameter=file_path>
README.md
</parameter>
</function>

That's all!`,
			expected: `Let me check the files:

🔧 shell_command

Now let me read a file:

🔧 read_file

That's all!`,
		},
		{
			name:     "regular text without tool calls",
			input:    `This is regular text without any tool calls.`,
			expected: `This is regular text without any tool calls.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sf.filterXMLToolCalls(tt.input)
			assert.Equal(t, tt.expected, result, "XML tool call filtering failed for: %s", tt.name)
		})
	}
}
