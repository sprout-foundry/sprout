package components

import (
	"context"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
)

// TestRealTerminalSimulation simulates the exact sequence that happens in real terminal usage
func TestRealTerminalSimulation(t *testing.T) {
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

	t.Logf("=== After setup - Cursor should be at top of content area ===")
	t.Logf("Scroll region: %d-%d", top, bottom)
	t.Logf("Cursor position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// Clear commands to focus on our sequence
	mockTerminal.commands = []string{}

	// NOW - Simulate the exact sequence that happens during startup:

	// 1. Show welcome message (this is what happens first in Start())
	t.Logf("\n=== Step 1: Welcome Message ===")
	ac.showWelcomeMessage()

	t.Logf("After welcome message:")
	t.Logf("Cursor position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Check where welcome message appeared
	lines := mockTerminal.GetBufferContent()
	welcomeLineNum := -1
	for i, line := range lines {
		if strings.Contains(line, "Welcome to Ledit Agent") {
			welcomeLineNum = i + 1
			break
		}
	}
	t.Logf("Welcome message found on line: %d", welcomeLineNum)

	// 2. Show initial help (this happens in Start())
	t.Logf("\n=== Step 2: Initial Help ===")
	mockTerminal.commands = []string{} // Clear commands

	ac.safePrint("   Press Esc to interrupt the agent.\n\n")

	t.Logf("After initial help:")
	t.Logf("Cursor position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// 3. Simulate footer render (this happens continuously)
	t.Logf("\n=== Step 3: Footer Render ===")
	mockTerminal.commands = []string{} // Clear commands

	// Simulate what the footer render might do
	// The footer might be positioning itself at the bottom
	footerTop := 21 // Lines 21-24 for footer
	mockTerminal.MoveCursor(1, footerTop)
	mockTerminal.WriteText("Footer content here")

	t.Logf("After footer render:")
	t.Logf("Cursor position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// 4. Now simulate user input processing
	t.Logf("\n=== Step 4: User Input Processing ===")
	mockTerminal.commands = []string{} // Clear commands

	// Simulate processInput being called (like when user types something)
	// This includes the processing message and agent response
	ac.safePrint("\r\033[K\n") // Clear line and move to new line
	ac.safePrint("🔄 Processing your request...\n")

	t.Logf("After processing message:")
	t.Logf("Cursor position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Simulate agent response
	ac.safePrint("Here's my response to your query.\n")
	ac.safePrint("This is line 2 of the response.\n")
	ac.safePrint("And this is line 3.\n")

	t.Logf("After agent response:")
	t.Logf("Cursor position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Final analysis
	t.Logf("\n=== FINAL ANALYSIS ===")
	t.Logf("Final cursor position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Expected content area: lines %d-%d", top, bottom)

	if mockTerminal.cursorY > bottom {
		t.Errorf("CURSOR WENT OUTSIDE CONTENT AREA! Cursor Y=%d, Content area bottom=%d", mockTerminal.cursorY, bottom)
	}

	// Accept cursor placement anywhere within the content area bounds

	// Print final buffer state
	t.Logf("\n=== FINAL BUFFER STATE ===")
	lines = mockTerminal.GetBufferContent()
	for i, line := range lines {
		if line != "" {
			t.Logf("Line %2d: '%s'", i+1, line)
		}
	}
}

// TestClearLineSequence tests the escape sequence processing that might be causing issues
func TestClearLineSequence(t *testing.T) {
	mockTerminal := NewMockTerminal(80, 24)

	// Test what happens when we write escape sequences
	t.Logf("=== Testing Escape Sequence Processing ===")

	mockTerminal.MoveCursor(5, 10) // Start at some position
	t.Logf("Initial position: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// Write the sequence that appears in processInput: "\r\033[K\n"
	mockTerminal.Write([]byte("\r\033[K\n"))

	t.Logf("After \\r\\033[K\\n sequence: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// The issue might be that the mock terminal doesn't handle escape sequences properly
	// In a real terminal, \033[K clears from cursor to end of line
	// \r moves cursor to beginning of line
	// But our mock might not process these correctly
}
