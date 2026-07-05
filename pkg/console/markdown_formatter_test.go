package console

import (
	"strings"
	"testing"
)

// TestMarkdownFormatter_TableRendering verifies that GitHub-flavored
// markdown tables are rendered as aligned columns without pipe borders.
func TestMarkdownFormatter_TableRendering(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	tests := []struct {
		name     string
		input    string
		contains []string
		notContains []string
	}{
		{
			name: "simple 2-column table renders aligned columns with header rule",
			input: "| File | Status |\n|------|--------|\n| a.go | ok |\n| b.go | fail |",
			contains: []string{
				"File", "Status", "a.go", "ok", "b.go", "fail",
				"─", // header rule
				ColorBold, // header row is bold
				ColorDim,  // separator is dim
			},
			notContains: []string{
				"|", // pipes should be removed
			},
		},
		{
			name: "3-column table with alignment markers",
			input: "| Name | Count | Aligned |\n|:---|---:|:---:|\n| left | 1 | center |\n| right | 99 | mid |",
			contains: []string{
				"Name", "Count", "Aligned",
				"left", "right", "1", "99", "center", "mid",
				"─",
			},
			notContains: []string{
				"|",
			},
		},
		{
			name: "cell with inline code — formatting applied",
			input: "| Key | Value |\n|-----|-------|\n| lang | `go` |\n| ver | **1.21** |",
			contains: []string{
				"Key", "Value", "lang", "ver",
				BgGray,  // inline code background
				ColorBold, // bold in "1.21"
			},
			notContains: []string{
				"|",
			},
		},
		{
			name: "table followed by normal paragraph",
			input: "| A | B |\n|---|---|\n| 1 | 2 |\n\nThis is after the table.",
			contains: []string{
				"A", "B", "1", "2",
				"This is after the table.",
			},
			notContains: []string{
				"|",
			},
		},
		{
			name: "single-row table (no separator) renders as plain text",
			input: "| Just one row |\n| with pipes |",
			contains: []string{
				"Just one row", "with pipes",
			},
			// No separator row means no table rendering — pipes may remain
			// or be stripped, but there should be no header rule.
			notContains: []string{},
		},
		{
			name: "empty cells handled gracefully",
			input: "| Col1 | Col2 |\n|------|------|\n| val | |\n| | val2 |",
			contains: []string{
				"Col1", "Col2", "val", "val2",
			},
			notContains: []string{
				"|",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.Format(tt.input)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected result to contain %q, but got:\n%s", expected, result)
				}
			}
			for _, unexpected := range tt.notContains {
				if strings.Contains(result, unexpected) {
					t.Errorf("Expected result to NOT contain %q, but got:\n%s", unexpected, result)
				}
			}
		})
	}
}

// TestMarkdownFormatter_TableNoColor verifies that tables in no-color mode
// strip pipes and still produce aligned columns.
func TestMarkdownFormatter_TableNoColor(t *testing.T) {
	formatter := NewMarkdownFormatter(false, true)

	input := "| File | Lines | Status |\n|------|-------|--------|\n| a.go | 42 | ok |"
	result := formatter.Format(input)

	// Should contain no ANSI codes
	if strings.Contains(result, "\033[") {
		t.Errorf("Expected no ANSI codes when colors disabled, got: %s", result)
	}

	// Should contain the table content
	if !strings.Contains(result, "File") || !strings.Contains(result, "a.go") {
		t.Errorf("Expected table content to remain, got: %s", result)
	}

	// Pipes should be stripped
	if strings.Contains(result, "|") {
		t.Errorf("Expected pipes to be stripped, got: %s", result)
	}
}

// TestMarkdownFormatter_TableWidthClamping verifies that column widths are
// clamped to fit within the formatter's configured width.
func TestMarkdownFormatter_TableWidthClamping(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true).SetWidth(30)

	input := "| VeryLongColumnName | AnotherVeryLongColumn |\n|-------------------|-----------------------|\n| short | longvalue |"
	result := formatter.Format(input)

	// Each line should be reasonably short (within configured width + margin)
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		// Strip ANSI for length check
		plain := stripANSIEscapeCodes(line)
		// Use rune count (display columns), not byte length.
		runeCount := len([]rune(plain))
		if runeCount > 40 {
			t.Errorf("Line too long (%d runes, expected ≤40): %q", runeCount, plain)
		}
	}
}

// TestMarkdownFormatter_NestedListIndentation verifies that nested lists
// render with proper visual indentation based on leading whitespace.
func TestMarkdownFormatter_NestedListIndentation(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	tests := []struct {
		name     string
		input    string
		contains []string // checked against ANSI-stripped output
	}{
		{
			name:  "2-level nested list: child indented under parent",
			input: "- parent\n  - child",
			contains: []string{
				"- parent",
				"  - child", // 2 spaces indent for 1 level
			},
		},
		{
			name:  "3-level nesting",
			input: "- parent\n  - child\n    - grandchild",
			contains: []string{
				"- parent",
				"  - child",      // 2 spaces = 1 level
				"    - grandchild", // 4 spaces = 2 levels
			},
		},
		{
			name:  "tab-indented child normalized to spaces",
			input: "- parent\n\t- child",
			contains: []string{
				"- parent",
				"- child", // tab (1 space) → level 0 → no indent
			},
		},
		{
			name:  "non-list line after nested list has no residual indent",
			input: "- item\n  - nested\nBack to normal text",
			contains: []string{
				"- item",
				"  - nested",
				"Back to normal text", // no leading spaces
			},
		},
		{
			name:  "mixed bullet types in nested list",
			input: "- parent\n  * child\n    + grandchild",
			contains: []string{
				"- parent",
				"  * child",
				"    + grandchild",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.Format(tt.input)
			// Strip ANSI codes for content checks.
			plain := stripANSIEscapeCodes(result)
			for _, expected := range tt.contains {
				if !strings.Contains(plain, expected) {
					t.Errorf("Expected result to contain %q, but got:\n%s", expected, plain)
				}
			}
		})
	}
}

// TestMarkdownFormatter_TableInCodeBlock verifies that pipe characters
// inside code blocks are NOT treated as table delimiters.
func TestMarkdownFormatter_TableInCodeBlock(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	input := "```text\n| not | a | table |\n|-----|---|------- |\n```\nAfter code block."
	result := formatter.Format(input)

	// The pipe characters should be preserved inside the code block
	if !strings.Contains(result, "| not | a | table |") {
		t.Errorf("Expected pipe characters to be preserved in code block, got:\n%s", result)
	}

	// Should have the code block gutter
	if !strings.Contains(result, "│ ") {
		t.Errorf("Expected code block gutter, got:\n%s", result)
	}
}

// TestMarkdownFormatter_TableAfterParagraph verifies that a table
// following a paragraph is detected correctly.
func TestMarkdownFormatter_TableAfterParagraph(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	input := "Here's a summary:\n\n| Metric | Value |\n|--------|-------|\n| CPU | 42% |\n| RAM | 64% |"
	result := formatter.Format(input)

	// Should contain the paragraph
	if !strings.Contains(result, "Here's a summary:") {
		t.Errorf("Expected paragraph text, got:\n%s", result)
	}

	// Should contain table content without pipes
	if !strings.Contains(result, "Metric") && !strings.Contains(result, "Value") {
		t.Errorf("Expected table headers, got:\n%s", result)
	}
	if strings.Contains(result, "| Metric") {
		t.Errorf("Expected pipes to be removed from table, got:\n%s", result)
	}
}

// TestMarkdownFormatter_TableEdgeCases covers edge cases like
// tables with extra whitespace and uneven column counts.
func TestMarkdownFormatter_TableEdgeCases(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	// Table with extra whitespace around pipes
	input := "  | Col1 | Col2 |  \n  |------|------|  \n  | val1 | val2 |  "
	result := formatter.Format(input)
	if !strings.Contains(result, "Col1") || !strings.Contains(result, "val1") {
		t.Errorf("Expected table content with extra whitespace, got:\n%s", result)
	}
}

// TestMarkdownFormatter_BasicFormatting verifies that the formatter
// produces the expected ANSI color codes and Unicode glyph markers for
// headers, bold, italic, lists, and code blocks.
//
// NewMarkdownFormatter respects NO_COLOR via envutil.ResolveColorPreference,
// so this test forces color mode on via t.Setenv. Without that, a
// developer shell with NO_COLOR=1 (or CI without TTY) produces a
// stripped output that contains none of the expected color codes.
func TestMarkdownFormatter_BasicFormatting(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:  "Headers",
			input: "# Main Title\n## Subtitle\n### Section",
			contains: []string{
				ColorBold + ColorBrightBlue,
				"█",
				ColorBold + ColorCyan,
				"▪",
				ColorBold + ColorBlue,
				"▸",
			},
		},
		{
			name:  "Inline formatting",
			input: "This has **bold** and *italic* and `code` text",
			contains: []string{
				ColorBold,
				ColorItalic,
				BgGray,
			},
		},
		{
			name:  "Lists",
			input: "- First item\n- Second item\n* Third item",
			contains: []string{
				ColorGreen + "-",
				ColorGreen + "*",
			},
		},
		{
			// Code-block decoration was lightened: one optional
			// "──── lang ────" header line and a dim "│ " gutter per
			// code row, instead of the previous four rows of chrome
			// (┌─/│ Language/│/└─). The language label and the
			// per-line gutter are still present, just without the
			// surrounding box.
			name:  "Code block",
			input: "```go\nfunc main() {\n  println(\"hello\")\n}\n```",
			contains: []string{
				"──── go ────",
				"│ ",
				ColorBlue + "func",
				ColorGreen + "hello",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.Format(tt.input)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected result to contain %q, but got:\n%s", expected, result)
				}
			}
		})
	}
}

// TestMarkdownFormatter_LongLineNotTruncated pins the Scanner buffer
// bump: a single line over 64 KiB (the default Scanner cap) must round-
// trip through Format without being silently dropped. Before the fix
// large generated diffs or one-line minified code in assistant output
// would simply vanish from the rendered turn.
func TestMarkdownFormatter_LongLineNotTruncated(t *testing.T) {
	formatter := NewMarkdownFormatter(true, true)
	// 100 KiB single-line payload — well past the 64 KiB Scanner default.
	long := strings.Repeat("abcdefghij", 100*1024/10)
	if len(long) < 64*1024 {
		t.Fatalf("test setup: expected >64KiB, got %d", len(long))
	}
	input := "before\n" + long + "\nafter"
	result := formatter.Format(input)
	if !strings.Contains(result, "before") {
		t.Errorf("missing prefix line in output")
	}
	if !strings.Contains(result, "after") {
		t.Errorf("missing suffix line — long line was silently dropped by the Scanner")
	}
	// The long line itself should also survive (sample a chunk of it).
	if !strings.Contains(result, strings.Repeat("abcdefghij", 100)) {
		t.Errorf("long line content missing from output")
	}
}

func TestMarkdownFormatter_DisabledColors(t *testing.T) {
	formatter := NewMarkdownFormatter(false, true)

	input := "# Title\n**bold** and `code`"
	result := formatter.Format(input)

	// Should contain no ANSI codes
	if strings.Contains(result, "\033[") {
		t.Errorf("Expected no ANSI codes when colors disabled, got: %s", result)
	}

	// Should contain stripped content
	if !strings.Contains(result, "Title") || !strings.Contains(result, "bold") {
		t.Errorf("Expected stripped content to remain, got: %s", result)
	}
}

func TestIsLikelyMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"plain text", false},
		{"# header", true},
		{"**bold**", true},
		{"`code`", true},
		{"```\ncode\n```", true},
		{"- list item", true},
		{"[link](url)", true},
		{"> quote", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsLikelyMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("IsLikelyMarkdown(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCIOutputHandler_Integration(t *testing.T) {
	handler := NewCIOutputHandler(&strings.Builder{})

	// Test markdown is passed through without processing
	markdown := "# Test\n**bold** text"
	_, err := handler.Write([]byte(markdown))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Markdown should be passed through unchanged (no processing)
}

func TestMarkdownFormatter_NO_COLOR_SuppressesANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	formatter := NewMarkdownFormatter(true, true)
	result := formatter.Format("# Title\n**bold** text")
	if strings.Contains(result, "\033[") {
		t.Errorf("NO_COLOR=1 should suppress all ANSI, but got: %q", result)
	}
}

func TestMarkdownFormatter_FORCE_COLOR_EnablesANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	formatter := NewMarkdownFormatter(false, true)
	result := formatter.Format("# Title\n**bold** text")
	if !strings.Contains(result, "\033[") {
		t.Errorf("FORCE_COLOR=1 should enable ANSI even when formatter created with false, but got: %q", result)
	}
}

func TestMarkdownFormatter_NO_COLOR_beats_FORCE_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "1")
	formatter := NewMarkdownFormatter(true, true)
	result := formatter.Format("# Title\n**bold** text")
	if strings.Contains(result, "\033[") {
		t.Errorf("NO_COLOR should win over FORCE_COLOR, but got ANSI in: %q", result)
	}
}

func TestMarkdownFormatter_UnderscoreItalicCommonMarkBoundaries(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	tests := []struct {
		name      string
		input     string
		hasItalic bool
	}{
		{
			name:      "standalone italic underscore",
			input:     "hello _world_ goodbye",
			hasItalic: true,
		},
		{
			name:      "identifier with underscores is NOT italic",
			input:     "handle_read_file",
			hasItalic: false,
		},
		{
			name:      "multiple underscores in identifier",
			input:     "some_long_function_name",
			hasItalic: false,
		},
		{
			name:      "underscore at end of identifier followed by space-underscore",
			input:     "func_name _is italic_",
			hasItalic: true,
		},
		{
			name:      "underscore at start then identifier",
			input:     "_italic_ then some_func",
			hasItalic: true,
		},
		{
			name:      "single underscore not followed by closing underscore",
			input:     "it's a contraction test_here",
			hasItalic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.Format(tt.input)
			if tt.hasItalic {
				if !strings.Contains(result, ColorItalic) {
					t.Errorf("Expected italic in %q but got none; output: %q", tt.input, result)
				}
			} else {
				if strings.Contains(result, ColorItalic) {
					t.Errorf("Did not expect italic in %q but got it; output: %q", tt.input, result)
				}
			}
		})
	}
}

// TestMarkdownFormatter_CodeBlockInsideList verifies that a fenced code
// block indented inside a list item is recognized as a code fence (not
// leaked as raw backticks) and rendered with the standard gutter and
// language header. This is valid CommonMark (e.g. "  ```go" inside a list).
func TestMarkdownFormatter_CodeBlockInsideList(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	input := "- item\n  ```go\n  fmt.Println(\"hi\")\n  ```"
	result := formatter.Format(input)

	// The code-block gutter should be present on code rows.
	if !strings.Contains(result, "│ ") {
		t.Errorf("Expected code block gutter (│ ) for indented fence, got:\n%s", result)
	}

	// The language header should be rendered.
	if !strings.Contains(result, "──── go ────") {
		t.Errorf("Expected '──── go ────' header for indented fence, got:\n%s", result)
	}

	// Raw backticks must NOT leak into the output.
	if strings.Contains(result, "```") {
		t.Errorf("Expected no raw backticks for indented fence, got:\n%s", result)
	}

	// The code content should appear (Go keyword "func" is NOT in this
	// snippet, but the code text should render). Check for the code text
	// without backticks — strip ANSI to verify the literal content.
	plain := stripANSIEscapeCodes(result)
	if !strings.Contains(plain, "fmt.Println") {
		t.Errorf("Expected code content 'fmt.Println' in output, got:\n%s", plain)
	}
}

// TestMarkdownFormatter_IndentedFenceGoKeyword verifies that an indented
// Go code fence renders highlighted Go keywords (e.g. "func").
func TestMarkdownFormatter_IndentedFenceGoKeyword(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	input := "- parent\n  ```go\n  func main() {\n    println(\"hello\")\n  }\n  ```\n- next"
	result := formatter.Format(input)

	// The func keyword should be highlighted.
	if !strings.Contains(result, ColorBlue+"func") {
		t.Errorf("Expected highlighted 'func' keyword in indented code block, got:\n%s", result)
	}

	// Code block gutter present.
	if !strings.Contains(result, "│ ") {
		t.Errorf("Expected code block gutter for indented fence, got:\n%s", result)
	}

	// No raw backticks leaked.
	if strings.Contains(result, "```") {
		t.Errorf("Expected no raw backticks, got:\n%s", result)
	}
}

// TestMarkdownFormatter_IndentedFenceCloserRules pins the CommonMark closing
// fence rule: a closing fence may be preceded by up to three spaces of
// indentation, independent of the opening fence's indentation.
func TestMarkdownFormatter_IndentedFenceCloserRules(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	t.Run("under-indented closer closes block", func(t *testing.T) {
		// Opener at 2 spaces, closer at 0 spaces: CommonMark closes.
		input := "- item\n  ```go\n  code\n```\nafter"
		result := formatter.Format(input)
		plain := stripANSIEscapeCodes(result)
		// "after" should render as plain text, not as a code row.
		if !strings.Contains(plain, "after") {
			t.Errorf("Expected 'after' to render as plain text after under-indented closer, got:\n%s", plain)
		}
		// No raw backticks should leak (the closer is honored).
		if strings.Contains(plain, "```") {
			t.Errorf("Expected no leaked backticks when closer is under-indented, got:\n%s", plain)
		}
	})

	t.Run("over-indented fence line stays open", func(t *testing.T) {
		// Opener at 2 spaces, fence-like line at 4 spaces: CommonMark
		// treats it as code content (stays open), so subsequent text is
		// also code until a valid closer appears.
		input := "  ```go\n  code\n    ```\n  ```\nafter"
		result := formatter.Format(input)
		plain := stripANSIEscapeCodes(result)
		// The over-indented "    ```" should render as a code row behind
		// the gutter, not close the block. Check that it appears with the
		// gutter (i.e. as code content).
		if !strings.Contains(plain, "│") {
			t.Errorf("Expected over-indented fence-like line to render as code content (gutter), got:\n%s", plain)
		}
	})

	t.Run("closer indented up to 3 spaces closes", func(t *testing.T) {
		// Opener at 1 space, closer at 3 spaces: CommonMark closes
		// (≤ 3 spaces allowed on the closer).
		input := " ```\n code\n   ```\nafter"
		result := formatter.Format(input)
		plain := stripANSIEscapeCodes(result)
		if !strings.Contains(plain, "after") {
			t.Errorf("Expected 'after' as plain text after 3-space closer, got:\n%s", plain)
		}
		if strings.Contains(plain, "```") {
			t.Errorf("Expected no leaked backticks with 3-space closer, got:\n%s", plain)
		}
	})
}

// TestMarkdownFormatter_Column0FenceAfterIndented pins the codeBlockIndent
// reset: a column-0 fence opened after an indented one must not inherit the
// prior indent (byte-identical to a fresh column-0 fence).
func TestMarkdownFormatter_Column0FenceAfterIndented(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	formatter := NewMarkdownFormatter(true, true)

	// Indented fence first, then a column-0 fence.
	input := "- a\n  ```go\n  x\n  ```\n```go\nfunc main() {}\n```"
	result := formatter.Format(input)
	plain := stripANSIEscapeCodes(result)

	// The column-0 fence's "func" keyword should render (highlighted, but we
	// check plain content).
	if !strings.Contains(plain, "func main()") {
		t.Errorf("Expected column-0 fence content after indented fence, got:\n%s", plain)
	}
	// No leaked backticks.
	if strings.Contains(plain, "```") {
		t.Errorf("Expected no leaked backticks, got:\n%s", plain)
	}
}
