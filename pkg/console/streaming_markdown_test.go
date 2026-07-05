package console

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStreamingMarkdown_Headers verifies that markdown headers are
// formatted with ANSI colors as they stream.
func TestStreamingMarkdown_Headers(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("# Big Header\n")
		r.WriteChunk("## Sub Header\n")
		r.WriteChunk("### Section\n")
		r.FinalizeAtTurnEnd()
	})
	// Headers should be bold and colored (not raw markdown).
	require.NotContains(t, out, "# Big", "raw # marker should be consumed")
	require.NotContains(t, out, "## Sub", "raw ## marker should be consumed")
	require.Contains(t, out, "Big Header")
	require.Contains(t, out, "Sub Header")
	require.Contains(t, out, "Section")
	// Should contain ANSI color codes.
	require.Contains(t, out, "\x1b[", "should have ANSI formatting")
}

// TestStreamingMarkdown_BoldAndItalic verifies inline formatting.
func TestStreamingMarkdown_BoldAndItalic(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("This is **bold** and *italic* text.\n")
		r.FinalizeAtTurnEnd()
	})
	// The ** and * markers should be consumed and replaced with ANSI codes.
	require.NotContains(t, out, "**", "bold markers should be consumed")
	require.NotContains(t, out, "*italic*", "italic markers should be consumed")
	require.Contains(t, out, "bold")
	require.Contains(t, out, "italic")
	require.Contains(t, out, "\x1b[1m", "should have bold ANSI code")
}

// TestStreamingMarkdown_InlineCode verifies backtick code spans.
func TestStreamingMarkdown_InlineCode(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("Run `npm install` to install.\n")
		r.FinalizeAtTurnEnd()
	})
	require.NotContains(t, out, "`npm", "backtick markers should be consumed")
	require.Contains(t, out, "npm install")
}

// TestStreamingMarkdown_CodeBlock verifies multi-line code blocks with
// language headers and syntax highlighting across chunk boundaries.
func TestStreamingMarkdown_CodeBlock(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("Here's some code:\n```go\n")
		r.WriteChunk("func main() {\n")
		r.WriteChunk("    fmt.Println(\"hello\")\n")
		r.WriteChunk("}\n```\n")
		r.WriteChunk("Done.\n")
		r.FinalizeAtTurnEnd()
	})
	// Language header should be present.
	require.Contains(t, out, "go")
	// Code content should be present (syntax highlighting inserts ANSI codes
	// between keywords and identifiers, so check for key fragments).
	require.Contains(t, out, "func")
	require.Contains(t, out, "main()")
	require.Contains(t, out, "Println")
	// Raw code fence markers should be consumed.
	require.NotContains(t, out, "```", "code fences should be consumed")
	// Code lines should have the dim gutter.
	require.Contains(t, out, "│", "code lines should have gutter")
	// Post-code prose should be present.
	require.Contains(t, out, "Done.")
}

// TestStreamingMarkdown_CodeBlockAcrossChunks verifies that code block
// state is maintained when content arrives in small chunks.
func TestStreamingMarkdown_CodeBlockAcrossChunks(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		// Split every character into its own chunk to stress-test buffering.
		for _, ch := range "Intro\n```python\nx = 1\n```\nOutro\n" {
			r.WriteChunk(string(ch))
		}
		r.FinalizeAtTurnEnd()
	})
	require.Contains(t, out, "Intro")
	require.Contains(t, out, "python")
	require.Contains(t, out, "x = 1")
	require.Contains(t, out, "Outro")
	require.NotContains(t, out, "```", "fences consumed")
}

// TestStreamingMarkdown_Table verifies that markdown tables are buffered
// and flushed as a formatted block when a non-table line arrives.
func TestStreamingMarkdown_Table(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("| Name | Age |\n")
		r.WriteChunk("|------|-----|\n")
		r.WriteChunk("| Alice | 30 |\n")
		r.WriteChunk("| Bob | 25 |\n")
		r.WriteChunk("\n") // blank line ends the table
		r.WriteChunk("After table.\n")
		r.FinalizeAtTurnEnd()
	})
	require.Contains(t, out, "Name")
	require.Contains(t, out, "Alice")
	require.Contains(t, out, "Bob")
	require.Contains(t, out, "After table.")
	// Pipe delimiters should be consumed by table formatting.
	// The table rows should not contain raw pipes as delimiters.
}

// TestStreamingMarkdown_BulletList verifies list formatting.
func TestStreamingMarkdown_BulletList(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("- First item\n")
		r.WriteChunk("- Second item\n")
		r.WriteChunk("- Third item\n")
		r.FinalizeAtTurnEnd()
	})
	require.Contains(t, out, "First item")
	require.Contains(t, out, "Second item")
	require.Contains(t, out, "Third item")
	// Bullet markers get colored — check that the green color is applied.
	require.Contains(t, out, "\x1b[32m", "bullets should be green")
}

// TestStreamingMarkdown_NoColorsRawEmit verifies that when colors are
// disabled (NewMarkdownFormatter(false, false)), text streams raw with
// only the indent — no formatting, no ANSI codes.
func TestStreamingMarkdown_NoColorsRawEmit(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("# Heading\n")
		r.WriteChunk("- bullet\n")
		r.WriteChunk("**bold**\n")
		r.FinalizeAtTurnEnd()
	})
	// With colors disabled, text should be raw markdown with indent only.
	require.Equal(t, "  # Heading\n  - bullet\n  **bold**\n", out)
	require.NotContains(t, out, "\x1b[", "no ANSI codes when colors disabled")
}

// TestStreamingMarkdown_PartialLineBuffered verifies that text without
// a trailing newline is buffered and not emitted until the line completes.
func TestStreamingMarkdown_PartialLineBuffered(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("partial") // no newline — should be buffered
	})
	// Nothing should be emitted yet — line is incomplete.
	require.Equal(t, "", out, "partial line should be buffered, not emitted")

	out2 := captureRendererStdout(t, func() {
		r.WriteChunk(" line\n") // completes the line
		r.FinalizeAtTurnEnd()
	})
	require.Contains(t, out2, "partial line", "completed line should be emitted")
}

// TestStreamingMarkdown_FinalizeFlushesPartialLine verifies that
// FinalizeAtTurnEnd flushes any remaining buffered partial line.
func TestStreamingMarkdown_FinalizeFlushesPartialLine(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("incomplete line without newline")
		r.FinalizeAtTurnEnd()
	})
	require.Contains(t, out, "incomplete line without newline")
	require.True(t, strings.HasSuffix(out, "\n"),
		"flushed partial line should end with \\n")
}

// TestStreamingMarkdown_HorizontalRule verifies width-aware rendering.
func TestStreamingMarkdown_HorizontalRule(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	r.formatter.SetWidth(80)
	out := captureRendererStdout(t, func() {
		r.WriteChunk("Above\n---\nBelow\n")
		r.FinalizeAtTurnEnd()
	})
	require.Contains(t, out, "Above")
	require.Contains(t, out, "Below")
	// The horizontal rule should be rendered as dim dashes.
	require.Contains(t, out, "─", "horizontal rule should be rendered")
}

// TestStreamingMarkdown_MultipleSegments verifies that formatter state
// resets properly between segments (after OnExternalWrite).
func TestStreamingMarkdown_MultipleSegments(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		// First segment with a code block.
		r.WriteChunk("```go\ncode1\n```\n")
		r.OnExternalWrite() // segment boundary
		// Second segment — should NOT be inside a code block.
		r.WriteChunk("regular text\n")
		r.FinalizeAtTurnEnd()
	})
	// First segment code block.
	require.Contains(t, out, "code1")
	// Second segment should be regular formatted text, not code.
	require.Contains(t, out, "regular text")
	// "regular text" should NOT have the code gutter.
	regIdx := strings.Index(out, "regular text")
	require.GreaterOrEqual(t, regIdx, 0)
	lineStart := strings.LastIndex(out[:regIdx], "\n")
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++ // skip the \n
	}
	prefix := out[lineStart:regIdx]
	require.NotContains(t, prefix, "│", "text after segment break should not have code gutter")
}

// TestStreamingMarkdownFormatter_Direct tests the streaming formatter
// in isolation (without the renderer).
func TestStreamingMarkdownFormatter_Direct(t *testing.T) {
	f := NewStreamingMarkdownFormatter(NewMarkdownFormatter(true, true))

	// Regular line.
	out := f.ProcessLine("hello world")
	require.Contains(t, out, "hello world")
	require.True(t, strings.HasSuffix(out, "\n"))

	// Code fence open — emits language header, no code gutter yet.
	out = f.ProcessLine("```python")
	require.Contains(t, out, "python")

	// Code line — should have the gutter.
	out = f.ProcessLine("x = 1")
	require.Contains(t, out, "│")
	require.Contains(t, out, "x = 1")

	// Code fence close — consumed, no output.
	out = f.ProcessLine("```")
	require.Empty(t, out)

	// Regular line after code block.
	out = f.ProcessLine("done")
	require.Contains(t, out, "done")
	require.NotContains(t, out, "│", "should not have code gutter after fence close")
}

// TestStreamingMarkdownFormatter_TableBuffering tests table state.
func TestStreamingMarkdownFormatter_TableBuffering(t *testing.T) {
	f := NewStreamingMarkdownFormatter(NewMarkdownFormatter(true, true))

	// First two table rows — buffered, no output.
	out1 := f.ProcessLine("| A | B |")
	require.Empty(t, out1, "first table row should be buffered")

	out2 := f.ProcessLine("|---|---|")
	require.Empty(t, out2, "separator row should be buffered")

	// Third table row — still buffered.
	out3 := f.ProcessLine("| 1 | 2 |")
	require.Empty(t, out3, "data row should be buffered")

	// Non-table line triggers flush + the new line.
	out4 := f.ProcessLine("after")
	require.Contains(t, out4, "A", "table should be flushed")
	require.Contains(t, out4, "1", "table data should be flushed")
	require.Contains(t, out4, "after", "new line should be emitted after table")
}
