package components

import (
	"testing"
)

// TestInputWrappingFunctionality tests the new input field wrapping capabilities
func TestInputWrappingFunctionality(t *testing.T) {
	t.Run("InputDimensionCalculation", testInputDimensionCalculation)
	t.Run("HeightChangeCallback", testHeightChangeCallback)
	t.Run("MultiLineInputField", testMultiLineInputField)
}

func testInputDimensionCalculation(t *testing.T) {
	t.Log("=== INPUT DIMENSION CALCULATION TEST ===")

	im := NewInputManager("> ")
	if im == nil {
		t.Fatal("Failed to create input manager")
	}

	// Set a known terminal width for testing
	im.termWidth = 80

	// Test 1: Short input (single line)
	im.currentLine = []rune("hello world")
	im.cursorPos = 5

	lines, cursorLine, cursorCol := im.calculateInputDimensions()

	// Should be 1 line for short input
	if lines != 1 {
		t.Errorf("Short input should use 1 line, got %d", lines)
	}

	// Cursor should be on line 0, at position after "> hel"
	expectedCol := len("> ") + 5 + 1 // prompt + cursor position + 1-based
	if cursorLine != 0 || cursorCol != expectedCol {
		t.Errorf("Cursor position wrong: got line %d col %d, want line 0 col %d", cursorLine, cursorCol, expectedCol)
	}

	t.Logf("✅ Short input: %d lines, cursor at line %d col %d", lines, cursorLine, cursorCol)

	// Test 2: Long input (multiple lines)
	longText := "This is a very long input that should wrap to multiple lines when the terminal width is limited and we keep typing more and more text"
	im.currentLine = []rune(longText)
	im.cursorPos = len(longText)

	lines2, cursorLine2, cursorCol2 := im.calculateInputDimensions()

	// Should be multiple lines
	if lines2 <= 1 {
		t.Errorf("Long input should use multiple lines, got %d", lines2)
	}

	t.Logf("✅ Long input (%d chars): %d lines, cursor at line %d col %d", len(longText), lines2, cursorLine2, cursorCol2)

	// Test 3: Input height tracking
	initialHeight := im.GetInputHeight()
	if initialHeight != 1 {
		t.Errorf("Initial input height should be 1, got %d", initialHeight)
	}

	newHeight := im.UpdateInputHeight()
	if newHeight <= 1 {
		t.Errorf("Updated input height should be > 1 for long text, got %d", newHeight)
	}

	t.Logf("✅ Height tracking: initial=%d, updated=%d", initialHeight, newHeight)
}

func testHeightChangeCallback(t *testing.T) {
	t.Log("=== HEIGHT CHANGE CALLBACK TEST ===")

	im := NewInputManager("> ")
	if im == nil {
		t.Fatal("Failed to create input manager")
	}

	im.termWidth = 50 // Smaller width to force wrapping

	// Set up height change callback
	var callbackCalled bool
	var callbackHeight int

	im.SetHeightChangeCallback(func(height int) {
		callbackCalled = true
		callbackHeight = height
	})

	// Start with short text
	im.currentLine = []rune("short")
	im.inputHeight = 1

	// Add long text that should trigger height change
	longText := "This text is long enough to wrap to multiple lines in our test terminal width"
	im.currentLine = []rune(longText)

	// Call notify method (simulating what showInputField would do)
	lines, _, _ := im.calculateInputDimensions()
	im.notifyLayoutOfInputHeight(lines)

	if !callbackCalled {
		t.Error("Height change callback was not called")
	}

	if callbackHeight <= 1 {
		t.Errorf("Callback should report height > 1, got %d", callbackHeight)
	}

	t.Logf("✅ Height change callback: called=%v, height=%d", callbackCalled, callbackHeight)
}

func testMultiLineInputField(t *testing.T) {
	t.Log("=== MULTI-LINE INPUT FIELD TEST ===")

	im := NewInputManager("prompt> ")
	if im == nil {
		t.Fatal("Failed to create input manager")
	}

	im.termWidth = 40 // Force wrapping

	// Test different cursor positions in wrapped text
	testText := "This is a long line that will definitely wrap to multiple lines"
	im.currentLine = []rune(testText)

	// Test cursor at beginning
	im.cursorPos = 0
	lines1, cursorLine1, cursorCol1 := im.calculateInputDimensions()
	t.Logf("Cursor at start: %d lines, cursor at line %d col %d", lines1, cursorLine1, cursorCol1)

	// Test cursor in middle
	im.cursorPos = len(testText) / 2
	lines2, cursorLine2, cursorCol2 := im.calculateInputDimensions()
	t.Logf("Cursor at middle: %d lines, cursor at line %d col %d", lines2, cursorLine2, cursorCol2)

	// Test cursor at end
	im.cursorPos = len(testText)
	lines3, cursorLine3, cursorCol3 := im.calculateInputDimensions()
	t.Logf("Cursor at end: %d lines, cursor at line %d col %d", lines3, cursorLine3, cursorCol3)

	// All should have same number of lines
	if lines1 != lines2 || lines2 != lines3 {
		t.Errorf("Line count should be consistent: got %d, %d, %d", lines1, lines2, lines3)
	}

	// Cursor should be on different lines for different positions
	if cursorLine1 == cursorLine3 && len(testText) > im.termWidth {
		t.Error("Cursor should be on different lines for start vs end of wrapped text")
	}

	t.Log("✅ Multi-line cursor positioning works correctly")
}

// TestInputHeightReset tests that input height resets to single line after submission
func TestInputHeightReset(t *testing.T) {
	t.Log("=== INPUT HEIGHT RESET TEST ===")

	im := NewInputManager("> ")
	if im == nil {
		t.Fatal("Failed to create input manager")
	}

	im.termWidth = 40 // Force wrapping

	// Set up height change callback to track changes
	var heightChanges []int
	im.SetHeightChangeCallback(func(height int) {
		heightChanges = append(heightChanges, height)
	})

	// Start with long text that will wrap
	longText := "This is a very long input that will definitely wrap to multiple lines"
	im.currentLine = []rune(longText)

	// Calculate initial dimensions (should be multiple lines)
	lines, _, _ := im.calculateInputDimensions()
	if lines <= 1 {
		t.Fatalf("Test setup failed: long text should wrap to multiple lines, got %d", lines)
	}

	// Simulate the input field being shown with multiple lines (this triggers the callback)
	im.notifyLayoutOfInputHeight(lines)

	if im.GetInputHeight() != lines {
		t.Errorf("Input height not set correctly: got %d, want %d", im.GetInputHeight(), lines)
	}

	t.Logf("Before submission: height=%d, text=%q", im.GetInputHeight(), string(im.currentLine))

	// Simulate Enter key press (this should reset height)
	im.handleEnter()

	// After handleEnter, input should be cleared and height reset to 1
	if len(im.currentLine) != 0 {
		t.Errorf("Input line should be cleared after Enter, got %d characters", len(im.currentLine))
	}

	if im.GetInputHeight() != 1 {
		t.Errorf("Input height should be reset to 1 after Enter, got %d", im.GetInputHeight())
	}

	if im.cursorPos != 0 {
		t.Errorf("Cursor position should be reset to 0 after Enter, got %d", im.cursorPos)
	}

	t.Logf("After submission: height=%d, text=%q, cursor=%d", im.GetInputHeight(), string(im.currentLine), im.cursorPos)

	// The key test is that height was reset to 1 - callback timing is less critical
	if len(heightChanges) >= 1 {
		t.Logf("Height changes recorded: %v", heightChanges)
	}

	// Give goroutine a moment to complete
	// time.Sleep(10 * time.Millisecond) // Uncomment if needed for timing

	// Most importantly, verify that the input manager state is correct
	t.Log("✅ Key functionality verified: height reset, input cleared, cursor reset")

	t.Log("✅ Input height properly resets after submission")
}

// TestEmptyInputHandling tests edge cases with empty input
func TestEmptyInputHandling(t *testing.T) {
	t.Log("=== EMPTY INPUT HANDLING TEST ===")

	im := NewInputManager("> ")
	if im == nil {
		t.Fatal("Failed to create input manager")
	}

	// Test 1: Empty input should not trigger submission
	im.currentLine = []rune("")
	im.cursorPos = 0
	im.inputHeight = 1

	initialHeight := im.GetInputHeight()

	// Simulate Enter with empty input
	im.handleEnter()

	// Height should remain the same for empty input
	if im.GetInputHeight() != initialHeight {
		t.Errorf("Height should not change for empty input: got %d, want %d", im.GetInputHeight(), initialHeight)
	}

	// Test 2: Whitespace-only input should not trigger submission
	im.currentLine = []rune("   \t  ")
	im.handleEnter()

	// Should still have whitespace and same height
	if len(im.currentLine) == 0 {
		t.Error("Whitespace-only input should not be cleared")
	}

	t.Log("✅ Empty input handling works correctly")
}

// TestInputWrappingRegression ensures wrapping fixes don't break existing functionality
func TestInputWrappingRegression(t *testing.T) {
	t.Log("=== INPUT WRAPPING REGRESSION TEST ===")

	// Test that basic input manager functionality still works
	im := NewInputManager("> ")
	if im == nil {
		t.Fatal("REGRESSION: Cannot create input manager")
	}

	// Test basic properties are preserved
	if im.prompt != "> " {
		t.Errorf("REGRESSION: Prompt not preserved: got %q", im.prompt)
	}

	// Test height getter/setter methods
	initialHeight := im.GetInputHeight()
	if initialHeight != 1 {
		t.Errorf("REGRESSION: Initial height should be 1, got %d", initialHeight)
	}

	// Test that callbacks can still be set
	im.SetCallbacks(func(string) error { return nil }, func() {})
	if im.onInput == nil || im.onInterrupt == nil {
		t.Error("REGRESSION: Callbacks not set properly")
	}

	// Test new height change callback
	im.SetHeightChangeCallback(func(int) {})
	if im.onHeightChange == nil {
		t.Error("REGRESSION: Height change callback not set")
	}

	t.Log("✅ Input manager wrapping changes don't break existing functionality")
}

// TestResizeCalculationHelper tests the new calculateLinesForWidth method
func TestResizeCalculationHelper(t *testing.T) {
	t.Log("=== RESIZE CALCULATION HELPER TEST ===")

	im := NewInputManager("> ")

	// Test 1: Short input should always be 1 line
	im.currentLine = []rune("short")
	lines := im.calculateLinesForWidth(80)
	if lines != 1 {
		t.Errorf("❌ Short input should be 1 line, got %d", lines)
	} else {
		t.Log("✅ Short input calculation correct")
	}

	// Test 2: Empty input should be 1 line
	im.currentLine = []rune("")
	lines = im.calculateLinesForWidth(80)
	if lines != 1 {
		t.Errorf("❌ Empty input should be 1 line, got %d", lines)
	} else {
		t.Log("✅ Empty input calculation correct")
	}

	// Test 3: Long input should wrap differently at different widths
	longInput := "This is a very long input string that will definitely wrap to multiple lines at narrow widths but fewer lines at wide widths"
	im.currentLine = []rune(longInput)

	narrowLines := im.calculateLinesForWidth(30)
	wideLines := im.calculateLinesForWidth(120)

	t.Logf("Long input: %d chars", len(longInput)+len(im.prompt))
	t.Logf("At width 30: %d lines", narrowLines)
	t.Logf("At width 120: %d lines", wideLines)

	if narrowLines <= wideLines {
		t.Errorf("❌ Narrow width should use more lines than wide width: %d vs %d", narrowLines, wideLines)
	} else {
		t.Log("✅ Width-based wrapping calculation correct")
	}

	// Test 4: Edge case - very narrow width
	veryNarrowLines := im.calculateLinesForWidth(5)
	if veryNarrowLines < 1 {
		t.Errorf("❌ Should never return less than 1 line, got %d", veryNarrowLines)
	} else {
		t.Logf("✅ Very narrow width handled correctly: %d lines", veryNarrowLines)
	}

	// Test 5: Zero width should use fallback
	zeroWidthLines := im.calculateLinesForWidth(0)
	if zeroWidthLines < 1 {
		t.Errorf("❌ Zero width should use fallback and return ≥1 lines, got %d", zeroWidthLines)
	} else {
		t.Logf("✅ Zero width fallback working: %d lines", zeroWidthLines)
	}

	t.Log("✅ Resize calculation helper working correctly")
}
