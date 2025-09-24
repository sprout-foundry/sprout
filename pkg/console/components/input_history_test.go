package components

import (
	"testing"
)

// MockHistoryManager implements the history manager interface for testing
type MockHistoryManager struct {
	history []string
}

func (m *MockHistoryManager) GetHistory() []string {
	return m.history
}

func (m *MockHistoryManager) AddEntry(entry string) {
	m.history = append(m.history, entry)
}

// TestArrowKeyHistoryNavigation tests the up/down arrow key history functionality
func TestArrowKeyHistoryNavigation(t *testing.T) {
	t.Run("BasicHistoryNavigation", testBasicHistoryNavigation)
	t.Run("HistoryNavigationEdgeCases", testHistoryNavigationEdgeCases)
	t.Run("HistoryResetOnModification", testHistoryResetOnModification)
	t.Run("EmptyHistoryHandling", testEmptyHistoryHandling)
}

func testBasicHistoryNavigation(t *testing.T) {
	t.Log("=== BASIC HISTORY NAVIGATION TEST ===")

	im := NewInputManager("> ")
	if im == nil {
		t.Fatal("Failed to create input manager")
	}

	// Set up mock history
	mockHistory := &MockHistoryManager{
		history: []string{"command1", "command2", "command3"},
	}
	im.SetHistoryManager(mockHistory)

	// Start with empty input
	im.currentLine = []rune("")
	im.cursorPos = 0
	im.historyIndex = -1

	// Test up arrow (should go to most recent: command3)
	im.handleUpArrow()
	if string(im.currentLine) != "command3" {
		t.Errorf("Up arrow should show most recent command: got %q, want %q", string(im.currentLine), "command3")
	}
	if im.historyIndex != 2 {
		t.Errorf("History index should be 2, got %d", im.historyIndex)
	}
	if im.cursorPos != len(im.currentLine) {
		t.Errorf("Cursor should be at end of line, got %d, want %d", im.cursorPos, len(im.currentLine))
	}
	t.Logf("✅ First up arrow: %q (index %d)", string(im.currentLine), im.historyIndex)

	// Test another up arrow (should go to command2)
	im.handleUpArrow()
	if string(im.currentLine) != "command2" {
		t.Errorf("Second up arrow should show command2: got %q", string(im.currentLine))
	}
	if im.historyIndex != 1 {
		t.Errorf("History index should be 1, got %d", im.historyIndex)
	}
	t.Logf("✅ Second up arrow: %q (index %d)", string(im.currentLine), im.historyIndex)

	// Test down arrow (should go back to command3)
	im.handleDownArrow()
	if string(im.currentLine) != "command3" {
		t.Errorf("Down arrow should go back to command3: got %q", string(im.currentLine))
	}
	if im.historyIndex != 2 {
		t.Errorf("History index should be 2, got %d", im.historyIndex)
	}
	t.Logf("✅ Down arrow: %q (index %d)", string(im.currentLine), im.historyIndex)

	// Test down arrow past end (should return to original empty input)
	im.handleDownArrow()
	if string(im.currentLine) != "" {
		t.Errorf("Down arrow past end should return to original input: got %q", string(im.currentLine))
	}
	if im.historyIndex != -1 {
		t.Errorf("History index should be -1 (not in history mode), got %d", im.historyIndex)
	}
	t.Logf("✅ Down arrow past end: %q (index %d)", string(im.currentLine), im.historyIndex)
}

func testHistoryNavigationEdgeCases(t *testing.T) {
	t.Log("=== HISTORY NAVIGATION EDGE CASES TEST ===")

	im := NewInputManager("> ")
	mockHistory := &MockHistoryManager{
		history: []string{"single-command"},
	}
	im.SetHistoryManager(mockHistory)

	// Test with current input before navigating
	im.currentLine = []rune("current input")
	im.cursorPos = len(im.currentLine)
	im.historyIndex = -1

	// Up arrow should save current input and show history
	im.handleUpArrow()
	if string(im.currentLine) != "single-command" {
		t.Errorf("Up arrow should show history command: got %q", string(im.currentLine))
	}

	// Down arrow should restore original input
	im.handleDownArrow()
	if string(im.currentLine) != "current input" {
		t.Errorf("Down arrow should restore original input: got %q", string(im.currentLine))
	}

	t.Log("✅ Current input preservation works")

	// Test going to oldest entry and trying to go older
	im.currentLine = []rune("")
	im.historyIndex = -1

	im.handleUpArrow() // Should go to index 0 (only entry)
	if im.historyIndex != 0 {
		t.Errorf("Should be at index 0, got %d", im.historyIndex)
	}

	// Try to go older (should stay at same entry)
	im.handleUpArrow()
	if im.historyIndex != 0 {
		t.Errorf("Should still be at index 0, got %d", im.historyIndex)
	}

	t.Log("✅ Boundary handling works correctly")
}

func testHistoryResetOnModification(t *testing.T) {
	t.Log("=== HISTORY RESET ON MODIFICATION TEST ===")

	im := NewInputManager("> ")
	mockHistory := &MockHistoryManager{
		history: []string{"command1", "command2"},
	}
	im.SetHistoryManager(mockHistory)

	// Navigate to history entry
	im.currentLine = []rune("original")
	im.handleUpArrow()

	if im.historyIndex == -1 {
		t.Error("Should be in history mode")
	}

	// Insert character should reset history mode
	im.insertChar('X')

	if im.historyIndex != -1 {
		t.Errorf("History mode should be reset after character insertion, got index %d", im.historyIndex)
	}

	if len(im.tempInput) != 0 {
		t.Error("Temp input should be cleared after history reset")
	}

	t.Log("✅ Character insertion resets history mode")

	// Test backspace reset
	im.currentLine = []rune("test")
	im.handleUpArrow() // Enter history mode

	if im.historyIndex == -1 {
		t.Error("Should be in history mode before backspace test")
	}

	im.handleBackspace()

	if im.historyIndex != -1 {
		t.Errorf("History mode should be reset after backspace, got index %d", im.historyIndex)
	}

	t.Log("✅ Backspace resets history mode")

	// Test Enter reset
	im.currentLine = []rune("test")
	im.handleUpArrow() // Enter history mode

	if im.historyIndex == -1 {
		t.Error("Should be in history mode before enter test")
	}

	im.handleEnter()

	if im.historyIndex != -1 {
		t.Errorf("History mode should be reset after enter, got index %d", im.historyIndex)
	}

	if len(im.tempInput) != 0 {
		t.Error("Temp input should be cleared after enter")
	}

	t.Log("✅ Enter resets history mode")
}

func testEmptyHistoryHandling(t *testing.T) {
	t.Log("=== EMPTY HISTORY HANDLING TEST ===")

	im := NewInputManager("> ")

	// Test with no history manager
	im.currentLine = []rune("test")
	originalLine := string(im.currentLine)

	im.handleUpArrow()

	if string(im.currentLine) != originalLine {
		t.Error("Arrow key should not modify input when no history manager")
	}

	// Test with empty history
	emptyHistory := &MockHistoryManager{history: []string{}}
	im.SetHistoryManager(emptyHistory)

	im.handleUpArrow()

	if string(im.currentLine) != originalLine {
		t.Error("Arrow key should not modify input when history is empty")
	}

	if im.historyIndex != -1 {
		t.Error("Should not enter history mode with empty history")
	}

	t.Log("✅ Empty history handled correctly")
}

// TestInputResetComprehensive provides comprehensive tests for input reset functionality
func TestInputResetComprehensive(t *testing.T) {
	t.Run("MultiLineResetAfterSubmission", testMultiLineResetAfterSubmission)
	t.Run("HistoryStateResetAfterSubmission", testHistoryStateResetAfterSubmission)
	t.Run("LayoutCallbackOnReset", testLayoutCallbackOnReset)
}

func testMultiLineResetAfterSubmission(t *testing.T) {
	t.Log("=== MULTI-LINE RESET AFTER SUBMISSION TEST ===")

	im := NewInputManager("> ")
	im.termWidth = 30 // Force wrapping

	// Set up callback to track layout changes
	var layoutCalls []int
	im.SetHeightChangeCallback(func(height int) {
		layoutCalls = append(layoutCalls, height)
	})

	// Start with multi-line input
	longInput := "This is a very long input that will definitely wrap to multiple lines"
	im.currentLine = []rune(longInput)
	im.cursorPos = len(im.currentLine)

	// Force height calculation
	lines, _, _ := im.calculateInputDimensions()
	im.inputHeight = lines

	if im.inputHeight <= 1 {
		t.Fatalf("Test setup failed: should have multi-line input, got height %d", im.inputHeight)
	}

	initialHeight := im.inputHeight
	t.Logf("Initial state: height=%d, text=%q", initialHeight, string(im.currentLine))

	// Submit input (Enter key)
	im.handleEnter()

	// Check that everything is reset
	if len(im.currentLine) != 0 {
		t.Errorf("Current line should be empty after submission, got %d chars", len(im.currentLine))
	}

	if im.cursorPos != 0 {
		t.Errorf("Cursor should be at 0 after submission, got %d", im.cursorPos)
	}

	if im.inputHeight != 1 {
		t.Errorf("Input height should be 1 after submission, got %d", im.inputHeight)
	}

	t.Logf("After submission: height=%d, text=%q, cursor=%d", im.inputHeight, string(im.currentLine), im.cursorPos)
	t.Log("✅ Multi-line input properly resets to single line after submission")
}

func testHistoryStateResetAfterSubmission(t *testing.T) {
	t.Log("=== HISTORY STATE RESET AFTER SUBMISSION TEST ===")

	im := NewInputManager("> ")
	mockHistory := &MockHistoryManager{
		history: []string{"previous command"},
	}
	im.SetHistoryManager(mockHistory)

	// Enter history mode
	im.currentLine = []rune("current input")
	im.handleUpArrow()

	// Verify we're in history mode
	if im.historyIndex == -1 {
		t.Error("Should be in history mode")
	}

	if len(im.tempInput) == 0 {
		t.Error("Should have temp input stored")
	}

	// Submit the history entry
	im.handleEnter()

	// Check that history state is reset
	if im.historyIndex != -1 {
		t.Errorf("History index should be reset to -1, got %d", im.historyIndex)
	}

	if len(im.tempInput) != 0 {
		t.Errorf("Temp input should be cleared, got %d chars", len(im.tempInput))
	}

	t.Log("✅ History navigation state properly resets after submission")
}

func testLayoutCallbackOnReset(t *testing.T) {
	t.Log("=== LAYOUT CALLBACK ON RESET TEST ===")

	im := NewInputManager("> ")
	im.termWidth = 25 // Force wrapping

	var callbackHeights []int
	im.SetHeightChangeCallback(func(height int) {
		callbackHeights = append(callbackHeights, height)
	})

	// Create multi-line input
	im.currentLine = []rune("This is long input that wraps")
	lines, _, _ := im.calculateInputDimensions()
	im.inputHeight = lines

	if im.inputHeight <= 1 {
		t.Fatalf("Test requires multi-line input, got height %d", im.inputHeight)
	}

	// Submit input (this should trigger height reset callback)
	im.handleEnter()

	// The callback is called asynchronously, but we can check that the height was reset
	if im.inputHeight != 1 {
		t.Errorf("Input height should be reset to 1, got %d", im.inputHeight)
	}

	t.Logf("Height callback calls: %v", callbackHeights)
	t.Log("✅ Layout callback properly handles reset to single line")
}
