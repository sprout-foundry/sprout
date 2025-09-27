package components

import (
	"context"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
)

// TestFirstLinePositioningAfterQuery tests the issue where the first line appears on/below input line
func TestFirstLinePositioningAfterQuery(t *testing.T) {
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

	// Reset current content line tracking
	ac.currentContentLine = 0

	t.Logf("=== SETUP COMPLETE ===")
	t.Logf("Scroll region: %d-%d", top, bottom)
	t.Logf("Content area: lines %d-%d", top, bottom)

	// === Simulate the exact sequence from processInput ===

	// Step 1: Show welcome message first (this works fine)
	t.Logf("\n=== Step 1: Welcome Message (baseline) ===")
	ac.showWelcomeMessage()

	t.Logf("After welcome message, cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// Clear commands to focus on the problematic sequence
	mockTerminal.commands = []string{}

	// Step 2: This is the exact sequence from processInput that causes the issue
	t.Logf("\n=== Step 2: processInput sequence ===")

	// From processInput: Lock output and clear input line
	ac.outputMutex.Lock()

	// This is the problematic line from processInput:
	ac.safePrint("\r\033[K\n") // Clear the current input line and move to a new line

	firstLineCursor := mockTerminal.cursorY
	t.Logf("After \\r\\033[K\\n, cursor: (%d,%d)", mockTerminal.cursorX, firstLineCursor)
	t.Logf("Commands so far: %v", mockTerminal.GetCommands())

	// Show processing indicator
	ac.safePrint("🔄 Processing your request...\n")

	processingLineCursor := mockTerminal.cursorY
	t.Logf("After processing message, cursor: (%d,%d)", mockTerminal.cursorX, processingLineCursor)

	ac.outputMutex.Unlock()

	// Step 3: Now simulate what happens when streaming starts
	t.Logf("\n=== Step 3: First streaming content ===")

	// Reset formatter (from processInput)
	ac.streamingFormatter.Reset()

	// The first line of streaming content - this is where the issue occurs
	mockTerminal.commands = []string{} // Clear to see exactly what happens

	ac.streamingFormatter.Write("Here's my first line of response\n")

	firstResponseLineCursor := mockTerminal.cursorY
	t.Logf("After first streaming line, cursor: (%d,%d)", mockTerminal.cursorX, firstResponseLineCursor)
	t.Logf("Commands during first streaming: %v", mockTerminal.GetCommands())

	// Step 4: Second line of streaming content
	ac.streamingFormatter.Write("This is the second line\n")

	secondResponseLineCursor := mockTerminal.cursorY
	t.Logf("After second streaming line, cursor: (%d,%d)", mockTerminal.cursorX, secondResponseLineCursor)

	// === ANALYSIS ===
	t.Logf("\n=== POSITION ANALYSIS ===")

	lines := mockTerminal.GetBufferContent()

	// Find where each piece of content appeared
	processingLineNum := -1
	firstResponseLineNum := -1
	secondResponseLineNum := -1

	for i, line := range lines {
		if strings.Contains(line, "Processing your request") {
			processingLineNum = i + 1
		}
		if strings.Contains(line, "Here's my first line") {
			firstResponseLineNum = i + 1
		}
		if strings.Contains(line, "This is the second line") {
			secondResponseLineNum = i + 1
		}
	}

	t.Logf("Processing message appears on line: %d", processingLineNum)
	t.Logf("First response line appears on line: %d", firstResponseLineNum)
	t.Logf("Second response line appears on line: %d", secondResponseLineNum)
	t.Logf("Content area is lines: %d-%d", top, bottom)

	// Print relevant buffer content
	t.Logf("\n=== BUFFER CONTENT ===")
	for i := top - 1; i <= bottom+2 && i < len(lines); i++ {
		if lines[i] != "" {
			marker := ""
			lineNum := i + 1
			if lineNum < top || lineNum > bottom {
				marker = " [OUTSIDE CONTENT AREA]"
			}
			t.Logf("Line %2d: '%s'%s", lineNum, lines[i], marker)
		}
	}

	// Check for the specific issue
	if firstResponseLineNum > bottom {
		t.Errorf("❌ ISSUE CONFIRMED: First response line appears outside content area (line %d > %d)", firstResponseLineNum, bottom)
		t.Errorf("   This means first line appears on/below input line before getting corrected")
	}

	// Account for streaming indicator that appears between processing message and first response
	expectedFirstResponseLine := processingLineNum + 2 // processing + streaming indicator + first response
	if firstResponseLineNum > expectedFirstResponseLine {
		t.Errorf("❌ ISSUE CONFIRMED: First response line appears much lower than expected (line %d vs expected ~%d)", firstResponseLineNum, expectedFirstResponseLine)
		t.Errorf("   This suggests positioning is getting confused during initial streaming")
	}

	// Success conditions
	if firstResponseLineNum >= top && firstResponseLineNum <= bottom && firstResponseLineNum == expectedFirstResponseLine {
		t.Logf("✅ First response line positioned correctly in content area")
	}

	if secondResponseLineNum == firstResponseLineNum+1 {
		t.Logf("✅ Second response line follows immediately after first")
	} else {
		t.Errorf("❌ Second response line positioning issue: line %d should be %d", secondResponseLineNum, firstResponseLineNum+1)
	}
}

// TestClearSequencePositioning specifically tests the \r\033[K\n sequence
func TestClearSequencePositioning(t *testing.T) {
	// Create mock terminal
	mockTerminal := NewMockTerminal(80, 24)
	mockAgent := &agent.Agent{}
	config := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(mockAgent, config)

	deps := console.Dependencies{
		Terminal: mockTerminal,
		Layout:   ac.autoLayoutManager,
	}

	ctx := context.Background()
	if err := ac.BaseComponent.Init(ctx, deps); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	ac.setupLayoutComponents()
	ac.autoLayoutManager.InitializeForTest(80, 24)
	ac.autoLayoutManager.SetComponentHeight("footer", 4)
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	mockTerminal.SetScrollRegion(top, bottom)
	mockTerminal.MoveCursor(1, top)
	ac.currentContentLine = 0

	t.Logf("=== CLEAR SEQUENCE TEST ===")
	t.Logf("Scroll region: %d-%d", top, bottom)

	// Add some initial content
	ac.safePrint("Some initial content\n")
	ac.safePrint("On multiple lines\n")

	initialCursor := mockTerminal.cursorY
	t.Logf("After initial content, cursor: (%d,%d)", mockTerminal.cursorX, initialCursor)

	// Clear commands to focus on the clear sequence
	mockTerminal.commands = []string{}

	// This is the problematic sequence from processInput
	t.Logf("\n=== Testing \\r\\033[K\\n sequence ===")
	ac.safePrint("\r\033[K\n")

	afterClearCursor := mockTerminal.cursorY
	t.Logf("After clear sequence, cursor: (%d,%d)", mockTerminal.cursorX, afterClearCursor)
	t.Logf("Commands during clear: %v", mockTerminal.GetCommands())

	// The issue might be that this sequence positions cursor incorrectly
	// Let's see where subsequent content appears
	ac.safePrint("Content after clear sequence\n")

	afterContentCursor := mockTerminal.cursorY
	t.Logf("After content following clear, cursor: (%d,%d)", mockTerminal.cursorX, afterContentCursor)

	// Check where the content appeared
	lines := mockTerminal.GetBufferContent()
	clearContentLineNum := -1
	for i, line := range lines {
		if strings.Contains(line, "Content after clear sequence") {
			clearContentLineNum = i + 1
			break
		}
	}

	t.Logf("Content after clear sequence appears on line: %d", clearContentLineNum)

	if clearContentLineNum > bottom {
		t.Errorf("❌ Clear sequence positioning issue: content appears outside content area (line %d > %d)", clearContentLineNum, bottom)
	} else {
		t.Logf("✅ Content after clear sequence appears in content area")
	}

	// The key insight: does the clear sequence mess up currentContentLine tracking?
	t.Logf("currentContentLine after clear sequence: %d", ac.currentContentLine)

	if ac.currentContentLine == 0 {
		t.Errorf("❌ currentContentLine reset to 0 by clear sequence - this will cause repositioning issues")
	} else {
		t.Logf("✅ currentContentLine properly maintained: %d", ac.currentContentLine)
	}
}
