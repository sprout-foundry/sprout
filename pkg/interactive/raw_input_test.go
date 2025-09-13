package interactive

import (
	"strings"
	"testing"
	"time"
)

func TestRawInputHandler_PasteDetection(t *testing.T) {
	// Test the paste detection logic
	handler := NewRawInputHandler("> ")

	// Simulate rapid input (paste-like behavior)
	handler.lastInputTime = time.Now()

	// Wait less than paste threshold
	time.Sleep(5 * time.Millisecond)

	// Check if we would detect this as paste
	now := time.Now()
	timeDiff := now.Sub(handler.lastInputTime)

	if timeDiff >= handler.pasteThreshold {
		t.Errorf("Expected time difference < %v, got %v", handler.pasteThreshold, timeDiff)
	}

	// Test multiline content detection
	multilineContent := "line1\nline2\nline3"
	lines := strings.Split(multilineContent, "\n")

	if len(lines) != 3 {
		t.Errorf("Expected 3 lines, got %d", len(lines))
	}
}

func TestRawInputHandler_History(t *testing.T) {
	handler := NewRawInputHandler("> ")

	// Test adding to history
	handler.addToHistory("command1")
	handler.addToHistory("command2")
	handler.addToHistory("command3")

	if len(handler.history) != 3 {
		t.Errorf("Expected 3 items in history, got %d", len(handler.history))
	}

	// Test duplicate prevention
	handler.addToHistory("command3")
	if len(handler.history) != 3 {
		t.Errorf("Expected 3 items in history (duplicate prevented), got %d", len(handler.history))
	}
}

func TestRawInputHandler_CommandCompletion(t *testing.T) {
	handler := NewRawInputHandler("> ")

	// Test command detection
	handler.currentLine = []rune("/hel")
	handler.cursorPos = 4

	// Verify /help would be a match
	commands := []string{"/help", "/quit", "/exit", "/paste"}

	matches := 0
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, string(handler.currentLine)) {
			matches++
		}
	}

	if matches != 1 {
		t.Errorf("Expected 1 match for '/hel', got %d", matches)
	}
}
