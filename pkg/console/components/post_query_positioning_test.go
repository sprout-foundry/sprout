package components

import (
	"context"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
)

// TestPostQueryPositioning reproduces the specific issue: output appears below input after query processing
func TestPostQueryPositioning(t *testing.T) {
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
	t.Logf("Footer area: lines %d-%d", 21, 24)
	t.Logf("Initial cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// === STEP 1: Show welcome message (this works correctly) ===
	t.Logf("\n=== STEP 1: Welcome Message (Working) ===")
	ac.showWelcomeMessage()

	t.Logf("After welcome message:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// Clear commands to focus on the problematic part
	mockTerminal.commands = []string{}

	// === STEP 2: Simulate what happens during processInput ===
	// This is where the issue occurs - after processing a query

	t.Logf("\n=== STEP 2: Simulate processInput Flow ===")

	// This simulates the exact sequence that happens in processInput():

	// 2a. Lock output and clear input line (line from processInput)
	ac.outputMutex.Lock()
	ac.safePrint("\r\033[K\n") // Clear the current input line and move to a new line
	ac.safePrint("🔄 Processing your request...\n")
	ac.outputMutex.Unlock()

	t.Logf("After processInput setup:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// 2b. Simulate streaming formatter output (this might be where cursor gets repositioned)
	// The streaming formatter might be positioning output incorrectly

	// Reset formatter for new session (from processInput)
	ac.streamingFormatter.Reset()

	// The streaming formatter has a custom output function that calls safePrint
	// Let's simulate what it does:
	t.Logf("\n=== STREAMING FORMATTER OUTPUT ===")
	mockTerminal.commands = []string{} // Clear to see what streaming does

	// Simulate streaming content (what would come from LLM)
	ac.streamingFormatter.Write("Here is my response to your query.\n")
	ac.streamingFormatter.Write("This should appear in the content area.\n")
	ac.streamingFormatter.Write("But it might be appearing below the input instead.\n")

	t.Logf("After streaming formatter:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// 2c. Force flush (from processInput)
	ac.streamingFormatter.ForceFlush()

	t.Logf("After force flush:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// 2d. Final processing (from processInput)
	ac.outputMutex.Lock()
	ac.safePrint("\n")
	ac.agent.PrintConciseSummary() // This might also cause positioning issues
	ac.outputMutex.Unlock()

	t.Logf("After final processing:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// === ANALYSIS ===
	t.Logf("\n=== FINAL BUFFER ANALYSIS ===")
	lines := mockTerminal.GetBufferContent()

	// Print all content to see where everything ended up
	for i, line := range lines {
		if line != "" {
			t.Logf("Line %2d: '%s'", i+1, line)
		}
	}

	// Key analysis: Where did the streaming content appear?
	responseStartLine := -1
	for i, line := range lines {
		if strings.Contains(line, "Here is my response") {
			responseStartLine = i + 1
			break
		}
	}

	t.Logf("\n=== POSITIONING ANALYSIS ===")
	t.Logf("Content area: lines %d-%d", top, bottom)
	t.Logf("Footer area starts at: line %d", 21)
	t.Logf("Agent response starts at: line %d", responseStartLine)
	t.Logf("Final cursor position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// Check for the specific issue you described
	if responseStartLine > bottom {
		t.Errorf("❌ ISSUE REPRODUCED: Agent response starts at line %d (below content area %d-%d)", responseStartLine, top, bottom)
		t.Errorf("   This means output is appearing below input, outside scroll region")
	}

	if mockTerminal.cursorY >= 24 {
		t.Errorf("❌ ISSUE REPRODUCED: Cursor at bottom line %d (at or below footer)", mockTerminal.cursorY)
		t.Errorf("   This means output is stuck at very bottom, not scrolling")
	}

	if responseStartLine > 20 {
		t.Errorf("❌ ISSUE REPRODUCED: Response at line %d (in footer area %d+)", responseStartLine, 21)
		t.Errorf("   This means content is appearing under the footer")
	}

	// Success conditions
	if responseStartLine >= top && responseStartLine <= bottom {
		t.Logf("✅ Response appears in content area (line %d)", responseStartLine)
	}

	if mockTerminal.cursorY <= bottom {
		t.Logf("✅ Cursor stays in content area (Y=%d)", mockTerminal.cursorY)
	}
}

// TestStreamingFormatterPositioning specifically tests if streaming formatter is causing the issue
func TestStreamingFormatterPositioning(t *testing.T) {
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

	t.Logf("=== STREAMING FORMATTER POSITIONING TEST ===")
	t.Logf("Scroll region: %d-%d", top, bottom)

	// Clear commands to see exactly what streaming formatter does
	mockTerminal.commands = []string{}

	// The issue might be that streaming formatter bypasses proper positioning
	// Let's see what its output function does

	t.Logf("\n=== Test 1: Direct streamingFormatter.Write() ===")
	ac.streamingFormatter.Write("Direct streaming content\n")

	t.Logf("After direct streaming:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Check buffer
	lines := mockTerminal.GetBufferContent()
	directLine := -1
	for i, line := range lines {
		if strings.Contains(line, "Direct streaming content") {
			directLine = i + 1
			break
		}
	}

	t.Logf("Direct streaming content appeared on line: %d", directLine)

	if directLine > bottom {
		t.Errorf("❌ Streaming formatter output appears below content area (line %d > %d)", directLine, bottom)
	} else {
		t.Logf("✅ Streaming formatter output appears in content area")
	}

	// === Test 2: After some content exists ===
	t.Logf("\n=== Test 2: Streaming after existing content ===")

	// Add some existing content first
	ac.safePrint("Existing content line 1\n")
	ac.safePrint("Existing content line 2\n")

	currentCursor := mockTerminal.cursorY
	t.Logf("Cursor after existing content: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	mockTerminal.commands = []string{} // Clear

	// Now try streaming
	ac.streamingFormatter.Write("New streaming content\n")

	t.Logf("After streaming with existing content:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Find where new streaming content appeared
	lines = mockTerminal.GetBufferContent()
	streamingLine := -1
	for i, line := range lines {
		if strings.Contains(line, "New streaming content") {
			streamingLine = i + 1
			break
		}
	}

	t.Logf("New streaming content appeared on line: %d", streamingLine)
	t.Logf("Expected: should be around line %d (after existing content)", currentCursor)

	if streamingLine > currentCursor+2 {
		t.Errorf("❌ Streaming content appeared much lower than expected (line %d vs ~%d)", streamingLine, currentCursor)
		t.Errorf("   This suggests streaming formatter is repositioning incorrectly")
	}
}
