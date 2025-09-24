package components

import (
	"context"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
)

// TestCursorRepositioningFix tests that cursor stays in content area after ScrollOutput calls
func TestCursorRepositioningFix(t *testing.T) {
	// Create mock terminal (80x24 standard size)
	mockTerminal := NewMockTerminal(80, 24)

	// Create mock agent
	mockAgent := &agent.Agent{}

	// Create agent console
	config := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(mockAgent, config)

	// Create mock dependencies
	deps := console.Dependencies{
		Terminal: mockTerminal,
		Layout:   ac.autoLayoutManager,
	}

	// Initialize components manually for testing (bypass the terminal check)
	ctx := context.Background()

	// Initialize base component
	if err := ac.BaseComponent.Init(ctx, deps); err != nil {
		t.Fatalf("Failed to initialize base component: %v", err)
	}

	// Register components with layout manager
	ac.setupLayoutComponents()

	// Initialize layout manager for testing (bypasses terminal check)
	ac.autoLayoutManager.InitializeForTest(80, 24)

	// Set up terminal manually - this simulates setupTerminal()
	ac.autoLayoutManager.SetComponentHeight("footer", 4)
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	mockTerminal.SetScrollRegion(top, bottom)
	mockTerminal.MoveCursor(1, top)

	// Create a mock input manager interface to simulate its positioning behavior
	mockInputManager := &MockInputManager{
		terminal:       mockTerminal,
		inputFieldLine: 20, // Simulate input field at line 20 (below content area)
	}

	// Reset current content line tracking
	ac.currentContentLine = 0

	t.Logf("=== SETUP COMPLETE ===")
	t.Logf("Scroll region: %d-%d", top, bottom)
	t.Logf("Content area: lines %d-%d", top, bottom)
	t.Logf("Input field line: %d", mockInputManager.inputFieldLine)

	// === TEST THE PROBLEMATIC SEQUENCE ===

	// Step 1: Write some content normally (should work fine)
	t.Logf("\n=== Step 1: Normal content ===")
	ac.safePrint("Initial content in content area\n")

	t.Logf("After initial content, cursor at: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// Step 2: Simulate ScrollOutput() call (this was causing the issue)
	t.Logf("\n=== Step 2: Simulate ScrollOutput() behavior ===")
	mockTerminal.commands = []string{} // Clear to see what happens

	// Simulate what ScrollOutput() does - moves cursor to input field
	mockInputManager.ScrollOutput()

	cursorAfterScrollOutput := mockTerminal.cursorY
	t.Logf("After simulated ScrollOutput(), cursor at: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands during ScrollOutput: %v", mockTerminal.GetCommands())

	if cursorAfterScrollOutput != mockInputManager.inputFieldLine {
		t.Errorf("Expected cursor to be positioned at input field line %d, got %d", mockInputManager.inputFieldLine, cursorAfterScrollOutput)
	} else {
		t.Logf("✅ ScrollOutput() correctly positioned cursor at input field line %d", cursorAfterScrollOutput)
	}

	// Step 3: Call repositionCursorToContentArea() (this is our fix)
	t.Logf("\n=== Step 3: repositionCursorToContentArea() fix ===")
	mockTerminal.commands = []string{} // Clear to see what our fix does

	ac.repositionCursorToContentArea()

	cursorAfterReposition := mockTerminal.cursorY
	t.Logf("After repositionCursorToContentArea(), cursor at: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands during reposition: %v", mockTerminal.GetCommands())

	if cursorAfterReposition > bottom {
		t.Errorf("❌ FAILED: Cursor still outside content area (Y=%d > bottom=%d)", cursorAfterReposition, bottom)
	} else {
		t.Logf("✅ SUCCESS: Cursor repositioned to content area (Y=%d <= bottom=%d)", cursorAfterReposition, bottom)
	}

	// Step 4: Write more content to verify it appears in the right place
	t.Logf("\n=== Step 4: Content after fix ===")
	mockTerminal.commands = []string{} // Clear

	ac.safePrint("Content after cursor repositioning fix\n")
	ac.safePrint("This should appear in content area\n")

	t.Logf("After post-fix content, cursor at: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// === FINAL ANALYSIS ===
	t.Logf("\n=== FINAL BUFFER ANALYSIS ===")
	lines := mockTerminal.GetBufferContent()

	contentLines := []string{}
	for i, line := range lines {
		if line != "" && i+1 <= bottom { // Only content area lines
			contentLines = append(contentLines, line)
			t.Logf("Content line %2d: '%s'", i+1, line)
		}
	}

	// Check if post-fix content appears in content area
	postFixContentFound := false
	postFixLineNum := -1
	for i, line := range lines {
		if strings.Contains(line, "Content after cursor repositioning fix") {
			postFixContentFound = true
			postFixLineNum = i + 1
			break
		}
	}

	if !postFixContentFound {
		t.Errorf("❌ FAILED: Post-fix content not found in buffer")
	} else if postFixLineNum > bottom {
		t.Errorf("❌ FAILED: Post-fix content appears outside content area (line %d > %d)", postFixLineNum, bottom)
		t.Errorf("   This means the fix didn't work - content still appearing below input")
	} else {
		t.Logf("✅ SUCCESS: Post-fix content appears in content area (line %d)", postFixLineNum)
		t.Logf("   The cursor repositioning fix is working correctly!")
	}
}

// MockInputManager simulates the input manager's cursor positioning behavior
type MockInputManager struct {
	terminal       *MockTerminal
	inputFieldLine int
}

func (mim *MockInputManager) ScrollOutput() {
	// Simulate what the real InputManager.ScrollOutput() does:
	// It calls showInputField() which positions cursor at input field line
	mim.terminal.MoveCursor(1, mim.inputFieldLine)
	mim.terminal.commands = append(mim.terminal.commands, "MockInputManager.ScrollOutput() - positioned at input field")
}

// Other methods needed to satisfy InputManager interface (no-ops for testing)
func (mim *MockInputManager) Start() error                         { return nil }
func (mim *MockInputManager) Stop()                                {}
func (mim *MockInputManager) GetInterruptChannel() <-chan struct{} { return make(chan struct{}) }
func (mim *MockInputManager) SetProcessing(processing bool)        {}
func (mim *MockInputManager) SetLayoutManager(lm interface{})      {}
func (mim *MockInputManager) SetCallbacks(inputCallback func(string) error, interruptCallback func()) {
}
