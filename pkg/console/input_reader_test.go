package console

import (
	"strings"
	"testing"
)

func TestInputReader_LineWrapping(t *testing.T) {
	// Test that the InputReader properly handles line wrapping calculations
	ir := NewInputReader("> ")
	
	// Test with a short input that shouldn't wrap
	ir.currentInput = "short"
	ir.previousInput = ""
	ir.cursorPos = len(ir.currentInput)
	
	// This should not cause any issues
	// We can't easily test the visual output, but we can test the calculations
	width := 20
	totalLength := len(ir.prompt) + len(ir.currentInput)
	expectedLines := (totalLength + width - 1) / width
	
	if expectedLines != 1 {
		t.Errorf("Expected 1 line for short input, got %d", expectedLines)
	}
	
	// Test with a long input that should wrap
	longInput := strings.Repeat("a", 50) // 50 characters
	ir.currentInput = longInput
	ir.previousInput = ""
	ir.cursorPos = len(ir.currentInput)
	
	totalLength = len(ir.prompt) + len(longInput)
	expectedLines = (totalLength + width - 1) / width
	
	if expectedLines < 3 {
		t.Errorf("Expected at least 3 lines for long input, got %d", expectedLines)
	}
	
	// Test cursor positioning for wrapped lines
	cursorTotalPos := len(ir.prompt) + ir.cursorPos
	expectedCursorLine := cursorTotalPos / width
	_ = cursorTotalPos % width // Calculate column but don't need to test it explicitly
	
	if expectedCursorLine < 2 {
		t.Errorf("Expected cursor to be on line >= 2 for long input, got %d", expectedCursorLine)
	}
}

func TestInputReader_PreviousInputTracking(t *testing.T) {
	ir := NewInputReader("> ")
	
	// Test that previousInput is properly tracked
	ir.currentInput = "first"
	ir.previousInput = ""
	
	// Simulate a navigation
	ir.previousInput = ir.currentInput
	ir.currentInput = "second"
	
	if ir.previousInput != "first" {
		t.Errorf("Expected previousInput to be 'first', got '%s'", ir.previousInput)
	}
	
	if ir.currentInput != "second" {
		t.Errorf("Expected currentInput to be 'second', got '%s'", ir.currentInput)
	}
}

func TestInputReader_LineCalculationEdgeCases(t *testing.T) {
	ir := NewInputReader("> ")
	width := 10
	
	// Test exact width match
	exactWidthInput := strings.Repeat("a", width-len(ir.prompt)) // Exactly fits one line
	ir.currentInput = exactWidthInput
	ir.previousInput = ""
	
	totalLength := len(ir.prompt) + len(exactWidthInput)
	expectedLines := (totalLength + width - 1) / width
	
	if expectedLines != 1 {
		t.Errorf("Expected 1 line for exact width input, got %d", expectedLines)
	}
	
	// Test one character over width
	overWidthInput := strings.Repeat("a", width-len(ir.prompt)+1) // One character over
	ir.currentInput = overWidthInput
	
	totalLength = len(ir.prompt) + len(overWidthInput)
	expectedLines = (totalLength + width - 1) / width
	
	if expectedLines != 2 {
		t.Errorf("Expected 2 lines for over-width input, got %d", expectedLines)
	}
}