package console

import (
	"strings"
	"testing"
)

func TestConsoleBuffer_BasicFunctionality(t *testing.T) {
	buffer := NewConsoleBuffer(100)

	// Test adding lines
	buffer.AddLine("Hello world")
	buffer.AddLine("Second line")

	// Test terminal width setting
	buffer.SetTerminalWidth(80)

	// Get visible lines
	lines := buffer.GetVisibleLines(10)
	if len(lines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(lines))
	}

	if lines[0] != "Hello world" {
		t.Errorf("Expected 'Hello world', got '%s'", lines[0])
	}
}

func TestConsoleBuffer_LineWrapping(t *testing.T) {
	buffer := NewConsoleBuffer(100)
	buffer.SetTerminalWidth(10) // Very narrow for testing

	// Add a long line that should wrap
	longLine := "This is a very long line that should wrap"
	buffer.AddLine(longLine)

	lines := buffer.GetVisibleLines(10)
	if len(lines) < 2 {
		t.Errorf("Expected line to wrap into multiple lines, got %d lines", len(lines))
	}

	// Verify all content is preserved when concatenated
	concatenated := strings.Join(lines, "")
	// Remove spaces added during wrapping
	cleanConcatenated := strings.ReplaceAll(concatenated, " ", "")
	cleanOriginal := strings.ReplaceAll(longLine, " ", "")

	if !strings.Contains(cleanConcatenated, cleanOriginal) {
		t.Errorf("Content not preserved during wrapping")
	}
}

func TestConsoleBuffer_MaxLines(t *testing.T) {
	buffer := NewConsoleBuffer(5) // Small buffer for testing

	// Add more lines than max
	for i := 0; i < 10; i++ {
		buffer.AddLine("Line " + string(rune('0'+i)))
	}

	lines := buffer.GetVisibleLines(10)
	if len(lines) > 5 {
		t.Errorf("Expected max 5 lines to be kept, got %d", len(lines))
	}

	// Should contain the last 5 lines
	expected := "Line 5"
	if !strings.Contains(lines[0], expected) {
		t.Errorf("Expected buffer to contain recent lines, got %v", lines)
	}
}

func TestConsoleBuffer_Scrolling(t *testing.T) {
	buffer := NewConsoleBuffer(100)

	// Add several lines
	for i := 0; i < 10; i++ {
		buffer.AddLine("Line " + string(rune('0'+i)))
	}

	// Test scrolling
	buffer.ScrollUp(2)
	lines := buffer.GetVisibleLines(5)

	// Should show earlier lines when scrolled up
	if len(lines) == 0 {
		t.Error("Expected lines after scrolling")
	}

	// Scroll back to bottom
	buffer.ScrollToBottom()
	linesAtBottom := buffer.GetVisibleLines(5)

	// Should show the last 5 lines
	if len(linesAtBottom) != 5 {
		t.Errorf("Expected 5 lines at bottom, got %d", len(linesAtBottom))
	}
}

func TestConsoleBuffer_TerminalResize(t *testing.T) {
	buffer := NewConsoleBuffer(100)

	// Add a line at one width
	buffer.SetTerminalWidth(20)
	buffer.AddLine("This is a medium length line for testing")

	lines20 := buffer.GetVisibleLines(10)

	// Change width and get lines again
	buffer.SetTerminalWidth(40)
	lines40 := buffer.GetVisibleLines(10)

	// Should have different wrapping
	if len(lines20) == len(lines40) {
		// This might be expected in some cases, but worth noting
		t.Logf("Line count didn't change with width: %d vs %d", len(lines20), len(lines40))
	}

	// Content should be preserved
	content20 := strings.Join(lines20, "")
	content40 := strings.Join(lines40, "")

	cleanContent20 := strings.ReplaceAll(content20, " ", "")
	cleanContent40 := strings.ReplaceAll(content40, " ", "")

	if cleanContent20 != cleanContent40 {
		t.Error("Content changed during resize")
	}
}

func TestConsoleBuffer_ANSIHandling(t *testing.T) {
	buffer := NewConsoleBuffer(100)
	buffer.SetTerminalWidth(10)

	// Add line with ANSI escape sequences
	coloredLine := "\033[31mRed text\033[0m normal"
	buffer.AddLine(coloredLine)

	lines := buffer.GetVisibleLines(5)
	if len(lines) == 0 {
		t.Error("Expected lines with ANSI sequences")
	}

	// Should preserve ANSI sequences in output
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "\033[31m") {
		t.Error("ANSI sequences should be preserved")
	}
}

func TestConsoleBuffer_MultilineContent(t *testing.T) {
	buffer := NewConsoleBuffer(100)

	// Add content with multiple lines
	multilineContent := "Line 1\nLine 2\nLine 3"
	buffer.AddContent(multilineContent)

	lines := buffer.GetVisibleLines(10)
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines from multiline content, got %d", len(lines))
	}

	expected := []string{"Line 1", "Line 2", "Line 3"}
	for i, expectedLine := range expected {
		if i >= len(lines) || lines[i] != expectedLine {
			t.Errorf("Line %d: expected '%s', got '%s'", i, expectedLine, lines[i])
		}
	}
}

func TestConsoleBuffer_Clear(t *testing.T) {
	buffer := NewConsoleBuffer(100)

	// Add some content
	buffer.AddLine("Test line 1")
	buffer.AddLine("Test line 2")

	// Verify content exists
	lines := buffer.GetVisibleLines(10)
	if len(lines) != 2 {
		t.Errorf("Expected 2 lines before clear, got %d", len(lines))
	}

	// Clear buffer
	buffer.Clear()

	// Verify buffer is empty
	linesAfterClear := buffer.GetVisibleLines(10)
	if len(linesAfterClear) != 0 {
		t.Errorf("Expected 0 lines after clear, got %d", len(linesAfterClear))
	}

	// Verify stats are reset
	stats := buffer.GetStats()
	if stats.TotalLines != 0 || stats.WrappedLines != 0 || stats.ScrollPosition != 0 {
		t.Errorf("Expected stats to be reset after clear: %+v", stats)
	}
}
